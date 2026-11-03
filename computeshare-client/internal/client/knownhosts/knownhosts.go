// Package knownhosts verifies broker-provided host keys and writes ~/.ssh/cs_known_hosts (node 002 R3–R5).
package knownhosts

import (
	"fmt"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/client/trust"
	"github.com/alishafiee1/computeshare-client/proto"
)

type Result struct {
	Lines    []string
	Problems []string
	Pinned   []string
}

// Build verifies each row: signature (R3), device-key TOFU pin (R4), owner fingerprint (R5).
// existing maps lower-name -> previous known_hosts line, kept when an update is refused.
func Build(rows []proto.NodeRow, ts *trust.Store, existing map[string]string) Result {
	var r Result
	for _, n := range rows {
		ln := strings.ToLower(n.Name)
		keep := func(msg string) {
			r.Problems = append(r.Problems, msg)
			if l, ok := existing[ln]; ok {
				r.Lines = append(r.Lines, l)
			}
		}
		if !proto.Verify(n.DevicePubkey, proto.RegistrationBytes(n.Name, n.HostKey, n.RegisteredAt), n.HostKeySig) {
			keep("signature invalid for " + n.Name)
			continue
		}
		fp, err := proto.Fingerprint(n.HostKey)
		if err != nil {
			keep("signature invalid for " + n.Name)
			continue
		}
		// owner-given fingerprint overrides broker data (R5)
		if want := ts.Fingerprint(n.Name); want != "" && want != fp {
			r.Problems = append(r.Problems, "fingerprint mismatch for "+n.Name)
			continue // never write a mismatching key (and drop any stale line)
		}
		if p := ts.Pinned(n.Name); p != "" && p != n.DevicePubkey && ts.Fingerprint(n.Name) == "" {
			keep(fmt.Sprintf("device key changed for %s; ask the owner", n.Name))
			continue
		}
		if ts.Pinned(n.Name) == "" {
			ts.Pin(n.Name, n.DevicePubkey)
			r.Pinned = append(r.Pinned, n.Name)
		}
		r.Lines = append(r.Lines, fmt.Sprintf("%s.cs,%s %s", ln, ln, strings.TrimSpace(n.HostKey)))
	}
	return r
}

// Parse reads an existing cs_known_hosts into lower-name -> line.
func Parse(s string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || strings.HasPrefix(l, "#") {
			continue
		}
		m[strings.TrimSuffix(strings.Split(f[0], ",")[0], ".cs")] = l
	}
	return m
}

// TOFUOnly lists the nodes in lines that have no owner-given fingerprint, i.e. that are
// trusted only because the broker said so the first time (client 017 R3).
func TOFUOnly(lines []string, ts *trust.Store) []string {
	var out []string
	for _, l := range lines {
		h, _, _ := strings.Cut(l, ",")
		n := strings.TrimSuffix(h, ".cs")
		if ts.Fingerprint(n) == "" {
			out = append(out, n)
		}
	}
	return out
}
