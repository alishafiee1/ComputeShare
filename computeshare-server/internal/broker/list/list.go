// Package list renders the node list for `ssh list@`, GET /v1/nodes and known_hosts (broker 003, node 002 R2).
package list

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
)

func pct(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", *p)
}

func dur(s int64) string {
	d := time.Duration(s) * time.Second
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

func age(sec int64) string {
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh", sec/3600)
	}
	return fmt.Sprintf("%dd", sec/86400)
}

// star marks a value that is the host's own total because the owner set no limit (card 016 R4).
func star(r proto.NodeRow, field string) string {
	for _, f := range r.Unlimited {
		if f == field {
			return "*"
		}
	}
	return ""
}

// Text renders R4 columns.
func Text(rows []proto.NodeRow, now int64) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tCPU\tRAM\tGPU\tLOAD now/avg24h\tNET-LIMIT\tUPTIME")
	if len(rows) == 0 {
		w.Flush()
		return b.String() + "no nodes\n"
	}
	for _, r := range rows {
		gpu := r.Specs.GPU
		if gpu == "" {
			gpu = "-"
		}
		ram := "-"
		if r.Specs.RAMGB > 0 {
			ram = fmt.Sprintf("%gG", r.Specs.RAMGB) + star(r, "ram")
		}
		cpu := r.Specs.CPU
		if cpu == "" {
			cpu = "-"
		} else {
			cpu += star(r, "cpu")
		}
		net := r.Limits.Bandwidth
		if net == "" {
			net = "unlimited"
		}
		status, up := r.Status, dur(r.UptimeS)
		if r.Status == "offline" || r.Status == "banned" {
			status = r.Status + " seen " + age(now-r.LastSeen)
			if r.LastSeen == 0 {
				status = r.Status + " seen never"
			}
			up = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s/%s\t%s\t%s\n", r.Name, status, cpu, ram, gpu, pct(r.Load.Now), pct(r.Load.Avg24h), net, up)
	}
	w.Flush()
	return b.String()
}

func JSON(rows []proto.NodeRow) string {
	if rows == nil {
		rows = []proto.NodeRow{}
	}
	b, _ := json.Marshal(rows)
	return string(b) + "\n"
}

// KnownHosts renders `<name>.cs,<name> <host_key>` lines with signature comments so
// clients can verify them (cs known-hosts verifies; this output alone is not trusted).
func KnownHosts(rows []proto.NodeRow) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "# cs-sig %s device_pubkey=%s registered_at=%d host_key_sig=%s\n", r.Name, r.DevicePubkey, r.RegisteredAt, r.HostKeySig)
		fmt.Fprintf(&b, "%s.cs,%s %s\n", strings.ToLower(r.Name), strings.ToLower(r.Name), strings.TrimSpace(r.HostKey))
	}
	return b.String()
}

const Usage = "usage: ssh list@SERVER [json|known_hosts]\n"
