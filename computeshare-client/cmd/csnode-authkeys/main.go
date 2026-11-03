// csnode-authkeys runs INSIDE the space as sshd AuthorizedKeysCommand (node 005 R4):
//
//	AuthorizedKeysCommand /usr/local/bin/csnode-authkeys %u %f
//
// It prints only those authorized_keys lines whose fingerprint the owner approved
// (/etc/csnode/approved, pushed by csnode on the host). An unapproved key that the
// owner listed in authorized_keys is recorded as pending in /var/lib/csnode/pending.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"
)

var userRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func main() {
	if len(os.Args) < 3 || !userRe.MatchString(os.Args[1]) {
		os.Exit(1)
	}
	user, offered := os.Args[1], os.Args[2]
	approved := map[string]bool{}
	if b, err := os.ReadFile("/etc/csnode/approved"); err == nil {
		for _, l := range strings.Fields(string(b)) {
			approved[l] = true
		}
	}
	f, err := os.Open(filepath.Join("/home", user, ".ssh", "authorized_keys"))
	if err != nil {
		os.Exit(0)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	listed := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		fp := ssh.FingerprintSHA256(pk)
		if fp == offered {
			listed = true
		}
		if approved[fp] {
			fmt.Println(line)
		}
	}
	if listed && !approved[offered] {
		name := strings.NewReplacer("/", "_", "+", "-", ":", "_").Replace(offered)
		os.WriteFile(filepath.Join("/var/lib/csnode/pending", name), []byte(offered+"\n"), 0o644)
	}
}
