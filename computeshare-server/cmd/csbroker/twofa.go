package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/alishafiee1/computeshare-server/internal/broker/store"
)

// admin2FA is `csbroker admin 2fa enable|disable|status` (server card 007). Local only: there is
// no way to turn two-step login off from the web panel.
func admin2FA(args []string) {
	if len(args) < 1 || (args[0] != "enable" && args[0] != "disable" && args[0] != "status") {
		usage()
	}
	fs := flag.NewFlagSet("admin 2fa", flag.ExitOnError)
	state := stateFlag(fs)
	user := fs.String("user", "admin", "admin username")
	code := fs.String("code", "", "enable: a code from the app (default: asked on the terminal)")
	fs.Parse(args[1:])
	if !proto.ValidUser(*user) {
		fmt.Fprintln(os.Stderr, "error: username must match ^[a-z_][a-z0-9_-]{0,31}$")
		os.Exit(1)
	}
	st := openStore(*state)
	defer st.Close()
	switch args[0] {
	case "status":
		if st.Admin2FAEnabled(*user) {
			fmt.Printf("2fa is ON for %s (%d backup codes left)\n", *user, st.Admin2FARemaining(*user))
		} else {
			fmt.Printf("2fa is off for %s\n", *user)
		}
	case "disable":
		if err := st.Admin2FADisable(*user); err != nil {
			fmt.Fprintln(os.Stderr, "error: two-step login was not enabled for", *user)
			os.Exit(1)
		}
		st.Audit("cli", "local", "admin.2fa.disable", *user)
		fmt.Printf("2fa removed for %s; its panel sessions were signed out\n", *user)
	case "enable":
		secret, err := st.Admin2FAStart(*user)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Println("Add this to your authenticator app (type the key, or use the address as a QR code):")
		fmt.Println("  key:     ", spaced(store.EncodeSecret(secret)))
		fmt.Println("  address: ", store.TOTPURI("ComputeShare", *user, secret))
		c := strings.TrimSpace(*code)
		if c == "" {
			c = strings.TrimSpace(readSecret("Code shown by the app: "))
		}
		codes, err := st.Admin2FAConfirm(*user, c)
		if err != nil {
			msg := err.Error()
			if errors.Is(err, store.ErrBad2FA) {
				msg = "that code is not valid; nothing was turned on (run enable again)"
			}
			fmt.Fprintln(os.Stderr, "error:", msg)
			os.Exit(1)
		}
		st.Audit("cli", "local", "admin.2fa.enable", *user)
		fmt.Printf("\n2fa is ON for %s. Backup codes (each works once; shown only now, store them offline):\n", *user)
		for _, bc := range codes {
			fmt.Println("  ", bc)
		}
	}
}

// spaced groups a base32 key in fours so it is easy to type.
func spaced(s string) string {
	var out []string
	for len(s) > 4 {
		out = append(out, s[:4])
		s = s[4:]
	}
	return strings.Join(append(out, s), " ")
}
