// Package sshconfig maintains the marked `Host *.cs` block in ~/.ssh/config (node 002 R1).
package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	Begin = "# >>> computeshare"
	End   = "# <<< computeshare"
)

// Mux reports whether ssh on this OS supports connection sharing; Windows OpenSSH does not (client 017 R4).
func Mux(goos string) bool { return goos != "windows" }

// MuxOptions reuse one connection per node for 10 minutes, so repeated ssh/scp skip the
// two handshakes through the jump (client 017 R4).
var MuxOptions = []string{"ControlMaster auto", "ControlPath ~/.ssh/cs-%C", "ControlPersist 10m"}

// Block renders the block of cli.md for server (host[:port]) and user; mux adds MuxOptions.
func Block(server, user string, mux bool) string {
	host, port := server, ""
	if h, p, ok := strings.Cut(server, ":"); ok && !strings.Contains(p, ":") {
		host, port = h, p
	}
	jump := "join@" + host
	if port != "" && port != "22" {
		jump = "-p " + port + " " + jump
	}
	extra := ""
	if mux {
		extra = "  " + strings.Join(MuxOptions, "\n  ") + "\n"
	}
	return fmt.Sprintf("%s\nHost *.cs\n  ProxyCommand ssh -W %%n:22 %s\n  User %s\n  UserKnownHostsFile ~/.ssh/cs_known_hosts\n  StrictHostKeyChecking yes\n%s%s\n", Begin, jump, user, extra, End)
}

// Apply inserts or replaces the marked block idempotently. It never edits text outside
// the markers; if an unmarked `Host *.cs` exists it returns a warning.
func Apply(path, block string) (warning string, err error) {
	os.MkdirAll(filepath.Dir(path), 0o700)
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	s := string(b)
	i, j := strings.Index(s, Begin), strings.Index(s, End)
	outside := s
	if i >= 0 && j > i {
		outside = s[:i] + s[j+len(End):]
	}
	for _, l := range strings.Split(outside, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && strings.EqualFold(f[0], "host") && strings.Contains(l, "*.cs") {
			warning = "warning: an existing 'Host *.cs' block outside the computeshare markers was left unchanged and may take precedence"
		}
	}
	var out string
	if i >= 0 && j > i {
		rest := s[j+len(End):]
		rest = strings.TrimPrefix(rest, "\n")
		out = s[:i] + block + rest
	} else {
		out = s
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += block
	}
	if out == s {
		return warning, nil
	}
	return warning, os.WriteFile(path, []byte(out), 0o600)
}
