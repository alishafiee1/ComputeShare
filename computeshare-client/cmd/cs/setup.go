package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/client/trust"
)

// validFingerprint reports whether fp looks like an OpenSSH SHA256 fingerprint: "SHA256:" and
// 43 characters of unpadded base64 (client 022 R2).
func validFingerprint(fp string) bool {
	rest, ok := strings.CutPrefix(fp, "SHA256:")
	if !ok || len(rest) != 43 {
		return false
	}
	for _, r := range rest {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '/':
		default:
			return false
		}
	}
	return true
}

const badFingerprint = "fingerprint must look like SHA256:<43 characters>"

// isIPHost reports whether the host part of a server address is an IP literal.
func isIPHost(server string) bool {
	h := server
	if sh, _, err := net.SplitHostPort(server); err == nil {
		h = sh
	}
	return net.ParseIP(strings.Trim(h, "[]")) != nil
}

// httpClient returns the client used for GET /v1/nodes. With a configured APIHost the
// certificate is checked against that name even when the API URL uses an IP (client 022 R3).
func httpClient(c Config) *http.Client {
	hc := &http.Client{Timeout: 20 * time.Second}
	if c.APIHost != "" {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{ServerName: c.APIHost, MinVersion: tls.VersionTLS12}}
	}
	return hc
}

// nodesRequest builds the GET /v1/nodes request, setting Host when APIHost is configured.
func nodesRequest(c Config) (*http.Request, error) {
	u := strings.TrimRight(c.API, "/") + "/v1/nodes"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.APIHost != "" {
		if pu, err := url.Parse(u); err == nil && net.ParseIP(pu.Hostname()) != nil {
			req.Host = c.APIHost
		}
	}
	return req, nil
}

// setupCmd is `cs setup`: init + setup-ssh + known-hosts in one idempotent command, without an
// invite (client card 019). `cs add` stays the way to pin a node with the owner's fingerprint.
func setupCmd(a []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	srv := fs.String("server", "", "broker SSH address host[:port] (default "+DefaultServer+")")
	api := fs.String("api", "", "broker HTTPS URL (default https://<host>)")
	user := fs.String("user", "", "default space user (default user)")
	host := fs.String("server-host", "", "DNS name for the TLS certificate when --server is an IP")
	id := fs.String("identity", "", "private key file ssh should use for *.cs (needed when it is not a default name)")
	fs.Parse(a)
	_, err := os.Stat(filepath.Join(cfgDir(), "config.json"))
	if err != nil || *srv != "" || *api != "" || *user != "" || *host != "" {
		s := *srv
		if s == "" {
			if c, e := loadOpt(); e == nil {
				s = c.Server
			} else {
				s = DefaultServer
			}
		}
		u := *user
		if u == "" {
			u = "user"
			if c, e := loadOpt(); e == nil && c.User != "" {
				u = c.User
			}
		}
		initCfg(s, *api, u, *host)
	}
	saveIdentity(*id)
	c := load()
	setupSSH(c)
	problems := knownHosts(c)
	rows := fetch(c)
	ts := trust.Load(cfgDir())
	first := ""
	for _, r := range rows {
		state := "server-only"
		if ts.Fingerprint(r.Name) != "" {
			state = "owner"
		}
		fmt.Printf("%-20s %-8s trusted: %s\n", r.Name, r.Status, state)
		if first == "" && r.Status == "online" {
			first = strings.ToLower(r.Name)
		}
	}
	if first == "" {
		fmt.Println("no nodes online")
	} else {
		fmt.Printf("ready: ssh %s.cs\n", first)
	}
	if problems > 0 {
		os.Exit(1)
	}
}
