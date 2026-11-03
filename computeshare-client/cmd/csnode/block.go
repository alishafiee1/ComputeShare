//go:build !windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/blocklist"
	"github.com/alishafiee1/computeshare-client/internal/node/config"
)

// cmdBlock is `csnode block add|ls|rm` (umbrella card 015 R11): the owner's block list of client
// addresses. The running agent reads the file, refuses new sessions from a blocked address and
// closes live ones within a few seconds.
func cmdBlock(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: csnode block add <ip|cidr> [--for 24h] [--reason text] | block ls | block rm <ip|cidr>")
		os.Exit(exitUsage)
	}
	l := blocklist.Open(config.Dir())
	switch args[0] {
	case "add":
		// flags may come after the address, so split them off by hand
		var addr string
		var rest []string
		for i := 1; i < len(args); i++ {
			if strings.HasPrefix(args[i], "-") {
				rest = append(rest, args[i])
				if !strings.Contains(args[i], "=") && i+1 < len(args) {
					rest = append(rest, args[i+1])
					i++
				}
			} else if addr == "" {
				addr = args[i]
			}
		}
		fs := flag.NewFlagSet("block add", flag.ExitOnError)
		dur := fs.Duration("for", 0, "block for this long, e.g. 24h (default: until removed)")
		why := fs.String("reason", "", "short note for yourself")
		fs.Parse(rest)
		if addr == "" {
			die(exitUsage, "usage: csnode block add <ip|cidr> [--for 24h] [--reason text]")
		}
		e, err := l.Add(addr, *dur, *why)
		if err != nil {
			die(exitErr, "%v", err)
		}
		until := "until removed"
		if e.Until != 0 {
			until = "until " + time.Unix(e.Until, 0).Format(time.RFC3339)
		}
		fmt.Printf("blocked %s %s (live sessions from it end within a few seconds)\n", e.Value, until)
	case "rm", "remove":
		if len(args) != 2 {
			die(exitUsage, "usage: csnode block rm <ip|cidr>")
		}
		ok, err := l.Remove(args[1])
		if err != nil {
			die(exitErr, "%v", err)
		}
		if !ok {
			die(exitErr, "%s is not on the block list", args[1])
		}
		fmt.Println("unblocked", args[1])
	case "ls", "list":
		es, err := l.Entries()
		if err != nil {
			die(exitErr, "%v", err)
		}
		if len(es) == 0 {
			fmt.Println("block list is empty")
			return
		}
		fmt.Printf("%-24s %-26s %s\n", "ADDRESS", "UNTIL", "REASON")
		for _, e := range es {
			u := "until removed"
			if e.Until != 0 {
				u = time.Unix(e.Until, 0).Format(time.RFC3339)
			}
			fmt.Printf("%-24s %-26s %s\n", e.Value, u, e.Reason)
		}
	default:
		die(exitUsage, "unknown block command %q", args[0])
	}
}

// blockLoop closes live sessions whose client address was added to the block list (R11: within 5 s).
func (a *agent) blockLoop(ctx context.Context, l *blocklist.List) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, s := range a.h.Sessions() {
				if l.Blocked(s.SrcIP) && a.h.Kick(s.ID) {
					a.log.Info("session closed: client address is blocked", "src_ip", s.SrcIP)
				}
			}
		}
	}
}
