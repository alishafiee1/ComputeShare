package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/release"
	"golang.org/x/term"
)

// releaseCmd implements card 012: keygen, sign, verify. The private key never leaves the owner's machine.
func releaseCmd(a []string) {
	if len(a) < 1 {
		usage()
	}
	switch a[0] {
	case "keygen":
		fs := flag.NewFlagSet("keygen", flag.ExitOnError)
		dir := fs.String("dir", cfgDir(), "where to write release.key and release.pub")
		fs.Parse(a[1:])
		kp, pp := filepath.Join(*dir, "release.key"), filepath.Join(*dir, "release.pub")
		if _, err := os.Stat(kp); err == nil {
			die(1, "%s already exists; refusing to overwrite a release key", kp)
		}
		pass := askPass("passphrase for the new release key (min 12): ")
		if string(askPass("repeat passphrase: ")) != string(pass) {
			die(1, "passphrases do not match")
		}
		priv, pub, err := release.Generate(pass)
		if err != nil {
			die(1, "%v", err)
		}
		os.MkdirAll(*dir, 0o700)
		if err := os.WriteFile(kp, priv, 0o600); err != nil {
			die(1, "%v", err)
		}
		os.WriteFile(pp, pub, 0o644)
		fmt.Printf("wrote %s (keep it here and on an offline USB stick only)\nwrote %s\n", kp, pp)
		fmt.Println("next: copy release.pub to computeshare-client/internal/release/release.pub and commit it")
	case "sign":
		fs := flag.NewFlagSet("sign", flag.ExitOnError)
		key := fs.String("key", filepath.Join(cfgDir(), "release.key"), "private key file")
		fs.Parse(a[1:])
		if fs.NArg() != 1 {
			usage()
		}
		kf, err := os.ReadFile(*key)
		if err != nil {
			die(1, "%v", err)
		}
		msg, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			die(1, "%v", err)
		}
		priv, err := release.Open(kf, askPass("release key passphrase: "))
		if err != nil {
			die(1, "%v", err) // wrong passphrase: no signature is written
		}
		out := fs.Arg(0) + ".sig"
		if err := os.WriteFile(out, release.Sign(priv, msg), 0o644); err != nil {
			die(1, "%v", err)
		}
		fmt.Println("wrote", out)
	case "verify":
		fs := flag.NewFlagSet("verify", flag.ExitOnError)
		pubf := fs.String("pub", "", "public key PEM (default: the key built into cs)")
		fs.Parse(a[1:])
		if fs.NArg() != 1 {
			usage()
		}
		msg, err1 := os.ReadFile(fs.Arg(0))
		sig, err2 := os.ReadFile(fs.Arg(0) + ".sig")
		if err1 != nil || err2 != nil {
			die(1, "need %s and %s.sig", fs.Arg(0), fs.Arg(0))
		}
		pub := release.Embedded()
		if *pubf != "" {
			b, err := os.ReadFile(*pubf)
			if err != nil {
				die(1, "%v", err)
			}
			if pub, err = release.ParsePublic(b); err != nil {
				die(1, "%v", err)
			}
		}
		if err := release.Verify(pub, msg, sig); err != nil {
			die(1, "%s: %v", fs.Arg(0), err)
		}
		fmt.Println(fs.Arg(0) + ": signature OK")
	default:
		usage()
	}
}

func askPass(prompt string) []byte {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			die(1, "%v", err)
		}
		return b
	}
	l, _ := stdin.ReadString('\n') // non-interactive (scripts, tests); never echoed by us
	return []byte(strings.TrimRight(l, "\r\n"))
}

var stdin = bufio.NewReader(os.Stdin)
