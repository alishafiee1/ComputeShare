// Package release signs and verifies release manifests (client card 012).
//
// The signing key is Ed25519. The private key file holds the seed sealed with a
// passphrase (scrypt + NaCl secretbox); it lives only on the owner's laptop and an
// offline USB backup (R2). The public key is a PEM "PUBLIC KEY" (PKIX), so the shell
// installer can verify with `openssl pkeyutl -verify -rawin` and needs no other tool.
// Signatures are the raw 64 bytes of Ed25519 over the exact file bytes.
package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/scrypt"
)

// embeddedPub is the project's release public key, committed after `cs release keygen` (R3).
// Empty means this build was made before a key existed: every verification then fails.
//
//go:embed release.pub
var embeddedPub []byte

// MinPassphrase is the minimum passphrase length for the private key file.
const MinPassphrase = 12

var (
	ErrNoKey     = errors.New("this build has no release public key (internal/release/release.pub is empty)")
	ErrBadSig    = errors.New("release signature is not valid")
	ErrBadPass   = errors.New("wrong passphrase or corrupt key file")
	ErrShortPass = fmt.Errorf("passphrase too short (min %d)", MinPassphrase)
)

// Embedded returns the build's release public key, or nil if none was embedded.
func Embedded() ed25519.PublicKey {
	pub, err := ParsePublic(embeddedPub)
	if err != nil {
		return nil
	}
	return pub
}

// EmbeddedPEM returns the PEM text of the embedded key ("" if none).
func EmbeddedPEM() string {
	if Embedded() == nil {
		return ""
	}
	return string(bytes.TrimSpace(embeddedPub)) + "\n"
}

type keyFile struct {
	V     int    `json:"v"`
	KDF   string `json:"kdf"`
	N     int    `json:"n"`
	R     int    `json:"r"`
	P     int    `json:"p"`
	Salt  []byte `json:"salt"`
	Nonce []byte `json:"nonce"`
	Box   []byte `json:"box"`
}

// scrypt cost; tests lower it.
var scryptN = 1 << 17

func derive(pass []byte, salt []byte, n, r, p int) (*[32]byte, error) {
	k, err := scrypt.Key(pass, salt, n, r, p, 32)
	if err != nil {
		return nil, err
	}
	var out [32]byte
	copy(out[:], k)
	return &out, nil
}

// Generate creates a new key pair and returns the sealed private key file and the PEM public key (R1).
func Generate(pass []byte) (priv, pub []byte, err error) {
	if len([]rune(string(pass))) < MinPassphrase {
		return nil, nil, ErrShortPass
	}
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	kf := keyFile{V: 1, KDF: "scrypt", N: scryptN, R: 8, P: 1, Salt: make([]byte, 16), Nonce: make([]byte, 24)}
	rand.Read(kf.Salt)
	rand.Read(kf.Nonce)
	key, err := derive(pass, kf.Salt, kf.N, kf.R, kf.P)
	if err != nil {
		return nil, nil, err
	}
	var nonce [24]byte
	copy(nonce[:], kf.Nonce)
	kf.Box = secretbox.Seal(nil, sk.Seed(), &nonce, key)
	priv, _ = json.MarshalIndent(kf, "", "  ")
	pub, err = MarshalPublic(pk)
	return priv, pub, err
}

// Open unseals a private key file.
func Open(file, pass []byte) (ed25519.PrivateKey, error) {
	var kf keyFile
	if err := json.Unmarshal(file, &kf); err != nil || kf.V != 1 || kf.KDF != "scrypt" || len(kf.Nonce) != 24 {
		return nil, ErrBadPass
	}
	key, err := derive(pass, kf.Salt, kf.N, kf.R, kf.P)
	if err != nil {
		return nil, ErrBadPass
	}
	var nonce [24]byte
	copy(nonce[:], kf.Nonce)
	seed, ok := secretbox.Open(nil, kf.Box, &nonce, key)
	if !ok || len(seed) != ed25519.SeedSize {
		return nil, ErrBadPass
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Sign returns the raw Ed25519 signature over msg (R4).
func Sign(priv ed25519.PrivateKey, msg []byte) []byte { return ed25519.Sign(priv, msg) }

// Verify checks a raw signature with pub (R5).
func Verify(pub ed25519.PublicKey, msg, sig []byte) error {
	if pub == nil {
		return ErrNoKey
	}
	if len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, msg, sig) {
		return ErrBadSig
	}
	return nil
}

// VerifyEmbedded checks a signature with the key built into this binary.
func VerifyEmbedded(msg, sig []byte) error { return Verify(Embedded(), msg, sig) }

func MarshalPublic(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func ParsePublic(b []byte) (ed25519.PublicKey, error) {
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PUBLIC KEY" {
		return nil, ErrNoKey
	}
	k, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("release key is not Ed25519")
	}
	return pub, nil
}

// VerifyFile checks that path is listed with the right SHA-256 in the SHA256SUMS next to it and that
// SHA256SUMS carries a valid signature by the embedded release key (R5: installer side, used by
// `csnode install` on Windows for the WSL rootfs).
func VerifyFile(path string) error {
	dir, name := filepath.Split(path)
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return fmt.Errorf("%w: SHA256SUMS missing next to %s", ErrBadSig, name)
	}
	sig, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS.sig"))
	if err != nil {
		return fmt.Errorf("%w: SHA256SUMS.sig missing", ErrBadSig)
	}
	if err := VerifyEmbedded(sums, sig); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	for _, l := range strings.Split(string(sums), "\n") {
		// sha256sum writes "<sum>  <name>" on Linux and "<sum> *<name>" (binary mode) in Git Bash on Windows
		sum, n, ok := strings.Cut(strings.TrimSpace(l), " ")
		if ok && strings.TrimPrefix(strings.TrimLeft(n, " "), "*") == name {
			if sum == got {
				return nil
			}
			return fmt.Errorf("%w: checksum mismatch for %s", ErrBadSig, name)
		}
	}
	return fmt.Errorf("%w: %s is not listed in SHA256SUMS", ErrBadSig, name)
}
