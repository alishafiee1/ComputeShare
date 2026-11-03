//go:build !windows

package main

// csnode users ... : per-user spaces (client 015 R4, R11). Accounts are created by the node
// owner and stay on this machine (decision 2026-10-30).

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/approval"
	"github.com/alishafiee1/computeshare-client/internal/node/config"
	"github.com/alishafiee1/computeshare-client/internal/node/gateway"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/internal/node/setup"
	"github.com/alishafiee1/computeshare-client/internal/node/users"
	"github.com/alishafiee1/computeshare-client/proto"
	"golang.org/x/crypto/ssh"
)

const usersUsage = `csnode users add <name> [--key-file f] [--no-password] [--disk 20GB]
csnode users ls
csnode users passwd <name>
csnode users keys add <name> <key-file|"ssh-ed25519 AAAA...">
csnode users keys ls <name>
csnode users keys remove <name> <key-name|SHA256:...>
csnode users disable|enable <name>
csnode users remove <name> [--purge]
csnode users migrate [--user user]   (turn on per-user spaces; moves the old single space to its user)`

func usersDie() { die(exitUsage, "%s", usersUsage) }

func cmdUsers(args []string) {
	if len(args) < 1 {
		usersDie()
	}
	c := mustCfg()
	switch args[0] {
	case "add":
		usersAdd(c, args[1:])
	case "ls":
		usersLs(c)
	case "passwd":
		if len(args) != 2 {
			usersDie()
		}
		pw := newPassword()
		must(users.Update(func(s *users.Store) error {
			u, ok := s.Get(args[1])
			if !ok {
				return users.ErrNotFound
			}
			return u.SetPassword(pw)
		}))
		fmt.Println("password changed for", args[1])
	case "keys":
		usersKeys(args[1:])
	case "disable", "enable":
		if len(args) != 2 {
			usersDie()
		}
		dis := args[0] == "disable"
		must(users.Update(func(s *users.Store) error {
			u, ok := s.Get(args[1])
			if !ok {
				return users.ErrNotFound
			}
			u.Disabled = dis
			return nil
		}))
		if dis && incus.Available() {
			incus.Stop(users.Instance(c.Name, args[1]), true) // ends live sessions now
		}
		fmt.Printf("%s %sd\n", args[1], args[0])
	case "remove":
		usersRemove(c, args[1:])
	case "migrate":
		usersMigrate(c, args[1:])
	default:
		usersDie()
	}
}

func must(err error) {
	if err != nil {
		die(exitErr, "%v", err)
	}
}

func newPassword() string {
	for {
		p := askSecret(fmt.Sprintf("password (min %d characters)", users.MinPassword))
		if len([]rune(p)) < users.MinPassword {
			fmt.Println(users.ErrShort)
			continue
		}
		if askSecret("again") != p {
			fmt.Println("passwords differ")
			continue
		}
		return p
	}
}

// readKey accepts a file path or the key text itself.
func readKey(arg string) string {
	if b, err := os.ReadFile(arg); err == nil {
		return strings.TrimSpace(string(b))
	}
	return arg
}

func usersAdd(c *config.Config, args []string) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		usersDie()
	}
	name := args[0]
	var keys []string
	noPw, disk := false, ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--no-password":
			noPw = true
		case "--key-file", "--disk":
			if i+1 >= len(args) {
				usersDie()
			}
			if args[i] == "--disk" {
				disk = args[i+1]
			} else {
				keys = append(keys, readKey(args[i+1]))
			}
			i++
		default:
			usersDie()
		}
	}
	if !users.ValidName(name) {
		die(exitErr, "%v", users.ErrName)
	}
	// edge case: never adopt an Incus instance csnode did not create for this user
	if incus.Available() && incus.Exists(users.Instance(c.Name, name)) {
		die(exitErr, "an Incus instance named %s already exists; pick another user name", users.Instance(c.Name, name))
	}
	pw := ""
	if !noPw {
		pw = newPassword()
	}
	must(users.Update(func(s *users.Store) error { return s.Add(name, pw, keys, disk) }))
	fmt.Printf("user %s added; their space is created on first login (ssh %s@%s.cs)\n", name, name, c.Name)
	if !c.Gateway {
		fmt.Println("note: per-user spaces are off on this node; turn them on with: csnode users migrate")
	}
}

func usersLs(c *config.Config) {
	s := users.Load()
	if len(s.Users) == 0 {
		fmt.Println("no users (add one with: csnode users add <name>)")
		return
	}
	fmt.Printf("%-16s %-9s %-5s %-4s %-8s %s\n", "USER", "STATE", "PASS", "KEYS", "DISK", "LAST LOGIN")
	for _, n := range s.Names() {
		u, _ := s.Get(n)
		state := "none"
		switch {
		case u.Removed:
			state = "removed"
		case u.Disabled:
			state = "disabled"
		case incus.Available():
			if st, err := incus.GetState(users.Instance(c.Name, n)); err == nil {
				state = strings.ToLower(st.Status)
			}
		}
		last := "-"
		if !u.LastSeen.IsZero() {
			last = u.LastSeen.Local().Format("2006-01-02 15:04")
		}
		disk := u.Disk
		if disk == "" {
			disk = "-"
		}
		fmt.Printf("%-16s %-9s %-5v %-4d %-8s %s\n", n, state, u.Hash != "", len(u.Keys), disk, last)
	}
}

func usersKeys(args []string) {
	if len(args) < 2 {
		usersDie()
	}
	op, name := args[0], args[1]
	switch op {
	case "add":
		if len(args) < 3 {
			usersDie()
		}
		key := readKey(strings.Join(args[2:], " "))
		must(users.Update(func(s *users.Store) error {
			u, ok := s.Get(name)
			if !ok {
				return users.ErrNotFound
			}
			return u.AddKey("", key)
		}))
		pk, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(key))
		fmt.Println("key added", ssh.FingerprintSHA256(pk))
	case "ls":
		u, ok := users.Load().Get(name)
		if !ok {
			die(exitErr, "%v", users.ErrNotFound)
		}
		for _, k := range u.Keys {
			fmt.Printf("%-16s %s  %s\n", k.Name, k.Fingerprint, k.Added.Format("2006-01-02"))
		}
	case "remove":
		if len(args) != 3 {
			usersDie()
		}
		must(users.Update(func(s *users.Store) error {
			u, ok := s.Get(name)
			if !ok {
				return users.ErrNotFound
			}
			if !u.RemoveKey(args[2]) {
				return fmt.Errorf("no key %s", args[2])
			}
			return nil
		}))
		fmt.Println("key removed; new logins with it are refused now")
	default:
		usersDie()
	}
}

func usersRemove(c *config.Config, args []string) {
	if len(args) < 1 {
		usersDie()
	}
	name, purge := args[0], len(args) > 1 && args[1] == "--purge"
	inst := users.Instance(c.Name, name)
	if !purge {
		must(users.Update(func(s *users.Store) error {
			u, ok := s.Get(name)
			if !ok {
				return users.ErrNotFound
			}
			u.Removed = true
			return nil
		}))
		if incus.Available() {
			incus.Stop(inst, true)
		}
		fmt.Printf("%s removed; the space is kept stopped (delete it with: csnode users remove %s --purge)\n", name, name)
		return
	}
	if ask(fmt.Sprintf("This deletes %s and all its files. Type the user name to confirm", inst), "") != name {
		die(exitErr, "aborted")
	}
	if incus.Available() && incus.Exists(inst) {
		must(incus.Delete(inst))
	}
	must(users.Update(func(s *users.Store) error { delete(s.Users, name); return nil }))
	fmt.Println(name, "and its space deleted")
}

// usersMigrate switches the node to per-user spaces (R11). An existing v2 space becomes the
// space of its Linux user; its keys move to users.json. The host key changes to the gateway key.
func usersMigrate(c *config.Config, args []string) {
	user := c.Space.User
	if len(args) == 2 && args[0] == "--user" {
		user = args[1]
	}
	if c.Gateway {
		fmt.Println("per-user spaces are already on")
		return
	}
	if incus.Available() && incus.Exists(c.Space.Instance) {
		m, err := setup.MigrateSingleSpace(c.Name, user, users.Instance(c.Name, user))
		if err != nil {
			die(exitErr, "migration failed: %v (nothing was switched; the old space is %s)", err, c.Space.Instance)
		}
		must(users.Update(func(s *users.Store) error {
			if _, ok := s.Get(user); ok {
				return nil
			}
			u := &users.User{Name: user, Created: time.Now().UTC()}
			for _, k := range m.Keys {
				u.AddKey("", k)
			}
			for _, k := range approval.Load().Keys { // approvals of 005 carry over
				u.ApproveKey(k)
			}
			s.Users[user] = u
			return nil
		}))
		fmt.Printf("moved %s to %s for user %s (%d keys)\n", c.Space.Instance, m.Instance, user, len(m.Keys))
		if m.HadPassword {
			fmt.Printf("the old password cannot be carried over; set one with: csnode users passwd %s\n", user)
		}
	}
	hk, err := gateway.LoadOrCreateHostKey(config.Path(gateway.HostKeyFile))
	must(err)
	c.Gateway = true
	must(c.Save())
	fp, _ := proto.Fingerprint(gateway.AuthorizedHostKey(hk))
	fmt.Println("per-user spaces: on. Restart the agent (csnode service install, or csnode run).")
	fmt.Println("new host key:", fp)
	fmt.Println("users must pin it again: give them the new line from `csnode status` (cs add ...)")
}

// cmdRelay is the helper csnode runs INSIDE a user space for direct-tcpip (015 R2).
func cmdRelay(args []string) {
	if len(args) != 2 {
		die(exitUsage, "csnode relay <host> <port>")
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(args[0], args[1]), 10*time.Second)
	if err != nil {
		die(exitErr, "connect: %v", err)
	}
	go func() { io.Copy(c, os.Stdin); c.(*net.TCPConn).CloseWrite() }()
	io.Copy(os.Stdout, c)
}
