package setup

// Iran mirror support (broker card 005 R4–R6, node side).

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/internal/release"
)

// Required paths a usable mirror must serve (edge case: partly filled → name the missing path).
var MirrorPaths = []string{"READY", "apt/debian/dists/bookworm/Release", "images/streams/v1/index.json"}

// ResolveMirror turns the --mirror flag into a base URL ("" = upstream).
// "auto" (default) = <server>/mirror when it answers, else upstream with a note.
func ResolveMirror(flag, server string, out func(string)) (string, error) {
	switch flag {
	case "off", "":
		return "", nil
	case "auto":
		m := strings.TrimRight(server, "/") + "/mirror"
		if err := CheckMirror(m); err != nil {
			out("note: no usable mirror at " + m + " (" + err.Error() + "); using upstream sources")
			return "", nil
		}
		return m, nil
	}
	m := strings.TrimRight(flag, "/")
	return m, CheckMirror(m)
}

func CheckMirror(m string) error {
	c := &http.Client{Timeout: 10 * time.Second}
	for _, p := range MirrorPaths {
		r, err := c.Head(m + "/" + p)
		if err != nil {
			return fmt.Errorf("mirror unreachable: %v", err)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			return fmt.Errorf("mirror is missing %s", p)
		}
	}
	return nil
}

// ErrImageTrust marks image checks that must stop setup instead of falling back (client 014 R4, R5).
var ErrImageTrust = errors.New("image trust check failed")

// ParseImageLock reads images.lock: "<alias> <arch> <sha256 fingerprint>" per line, # comments.
func ParseImageLock(b []byte) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) == 3 && !strings.HasPrefix(f[0], "#") && len(f[2]) == 64 {
			m[f[0]+" "+f[1]] = strings.ToLower(f[2])
		}
	}
	return m
}

// verifyLock checks images.lock.sig; tests swap in a throwaway key.
var verifyLock = release.VerifyEmbedded

func get(c *http.Client, url string) ([]byte, error) {
	r, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("%s: HTTP %d", url, r.StatusCode)
	}
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// LockedFingerprint returns the fingerprint for alias on this arch from the mirror's
// images.lock, after checking images.lock.sig with the release key (014 R3).
func LockedFingerprint(m, alias string) (string, error) {
	c := &http.Client{Timeout: 20 * time.Second}
	lock, err := get(c, m+"/images/images.lock")
	if err != nil {
		return "", fmt.Errorf("%w: mirror has no images.lock (%v)", ErrImageTrust, err)
	}
	sig, err := get(c, m+"/images/images.lock.sig")
	if err != nil {
		return "", fmt.Errorf("%w: mirror has no images.lock.sig (%v)", ErrImageTrust, err)
	}
	if err := verifyLock(lock, sig); err != nil {
		return "", fmt.Errorf("%w: images.lock: %v", ErrImageTrust, err)
	}
	fp, ok := ParseImageLock(lock)[alias+" "+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("%w: %s (%s) is not in images.lock; pass --image-fingerprint <sha256> to use it anyway", ErrImageTrust, alias, runtime.GOARCH)
	}
	return fp, nil
}

// MirrorImage maps images:X to the mirror, pinned to a fingerprint: the signed images.lock
// entry, or the owner's explicit --image-fingerprint (014 R4, R5). Upstream images (no
// mirror) are fetched over HTTPS from the official server and left as they are.
// Errors wrapping ErrImageTrust must abort setup; other errors mean "mirror unusable".
func MirrorImage(m, image, fingerprint string) (string, error) {
	if m == "" || !strings.HasPrefix(image, "images:") {
		return image, nil
	}
	alias := strings.TrimPrefix(image, "images:")
	fp := strings.ToLower(fingerprint)
	if fp == "" {
		var err error
		if fp, err = LockedFingerprint(m, alias); err != nil {
			return "", err
		}
	}
	incus.Run(nil, "remote", "remove", "csmirror")
	if _, err := incus.Run(nil, "remote", "add", "csmirror", m+"/images", "--protocol", "simplestreams", "--public"); err != nil {
		return "", err
	}
	// Incus checks the downloaded image against the fingerprint it was asked for.
	if _, err := incus.Run(nil, "image", "info", "csmirror:"+fp); err != nil {
		return "", fmt.Errorf("%w: image fingerprint mismatch for %s; the mirror may be tampered. Nothing was created.", ErrImageTrust, alias)
	}
	return "csmirror:" + fp, nil
}

// SpaceSourcesScript writes apt/pip/npm config inside the space (R5; owner may change later).
func SpaceSourcesScript(m string) string {
	if m == "" {
		return ""
	}
	return fmt.Sprintf(`. /etc/os-release
rm -f /etc/apt/sources.list.d/debian.sources
echo "deb %[1]s/apt/debian $VERSION_CODENAME main contrib non-free-firmware" > /etc/apt/sources.list
mkdir -p /etc/pip; printf '[global]\nindex-url = %[1]s/pypi/simple\n' > /etc/pip.conf
echo "registry=%[1]s/npm/" > /etc/npmrc
`, m)
}

// VerifyModel checks <dir>/SHA256SUMS (R6). Returns the list of bad/missing files.
func VerifyModel(dir string) ([]string, error) {
	f, err := os.Open(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var bad []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		sum, name, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		name = strings.TrimPrefix(strings.TrimLeft(name, " "), "*")
		g, err := os.Open(filepath.Join(dir, filepath.Clean("/"+name)))
		if err != nil {
			bad = append(bad, name+" (missing)")
			continue
		}
		h := sha256.New()
		io.Copy(h, g)
		g.Close()
		if hex.EncodeToString(h.Sum(nil)) != sum {
			bad = append(bad, name+" (checksum mismatch)")
		}
	}
	return bad, sc.Err()
}
