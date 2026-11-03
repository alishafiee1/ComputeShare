package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B (SHA-1, secret "12345678901234567890"): the 8-digit values end in these 6 digits.
func TestTOTPVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037"} {
		if got := totpCode(secret, ts/30); got != want {
			t.Fatalf("t=%d: got %s want %s", ts, got, want)
		}
	}
}

func TestVerifyTOTPWindowAndReuse(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111109, 0)
	cur := now.Unix() / 30
	code := totpCode(secret, cur)
	step, ok := verifyTOTP(secret, code, now, 0)
	if !ok || step != cur {
		t.Fatal("current code refused")
	}
	if _, ok := verifyTOTP(secret, code, now, step); ok {
		t.Fatal("a code was accepted twice")
	}
	if _, ok := verifyTOTP(secret, totpCode(secret, cur-1), now, 0); !ok {
		t.Fatal("previous step refused")
	}
	if _, ok := verifyTOTP(secret, totpCode(secret, cur+1), now, 0); !ok {
		t.Fatal("next step refused")
	}
	if _, ok := verifyTOTP(secret, totpCode(secret, cur-2), now, 0); ok {
		t.Fatal("a code two steps old was accepted")
	}
	if _, ok := verifyTOTP(secret, "12345", now, 0); ok {
		t.Fatal("short code accepted")
	}
	if _, ok := verifyTOTP(secret, " "+code[:3]+" "+code[3:], now, 0); !ok {
		t.Fatal("spaces in the code should be ignored")
	}
}

func TestTOTPURI(t *testing.T) {
	u := TOTPURI("ComputeShare", "admin", []byte("12345678901234567890"))
	if !strings.HasPrefix(u, "otpauth://totp/ComputeShare:admin?") || !strings.Contains(u, "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ") || !strings.Contains(u, "issuer=ComputeShare") {
		t.Fatal(u)
	}
}

func open2FA(t *testing.T) (*Store, string) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "csbroker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.AdminSetPassword("admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	return st, dir
}

// server 007 R1-R3: start, confirm with a valid code, ten backup codes, secret encrypted at rest.
func TestAdmin2FAEnableFlow(t *testing.T) {
	st, dir := open2FA(t)
	if st.Admin2FAEnabled("admin") {
		t.Fatal("enabled before setup")
	}
	secret, err := st.Admin2FAStart("admin")
	if err != nil {
		t.Fatal(err)
	}
	if st.Admin2FAEnabled("admin") {
		t.Fatal("enabled before the first code")
	}
	if _, err := st.Admin2FAConfirm("admin", "000000"); !errors.Is(err, ErrBad2FA) {
		t.Fatalf("wrong code confirmed: %v", err)
	}
	codes, err := st.Admin2FAConfirm("admin", totpCode(secret, st.Now().Unix()/30))
	if err != nil || len(codes) != 10 {
		t.Fatalf("confirm: %v %v", codes, err)
	}
	if !st.Admin2FAEnabled("admin") || st.Admin2FARemaining("admin") != 10 {
		t.Fatal("not enabled after confirm")
	}
	// the key file is private and the raw secret is not in the database file
	fi, err := os.Stat(filepath.Join(dir, "2fa.key"))
	if err != nil || fi.Size() != 32 {
		t.Fatalf("key file: %v", err)
	}
	var box []byte
	st.db.QueryRow(`SELECT secret_enc FROM admin_2fa WHERE username='admin'`).Scan(&box)
	if strings.Contains(string(box), string(secret)) || len(box) <= len(secret) {
		t.Fatal("secret stored in the clear")
	}
	if _, err := st.Admin2FAStart("nobody"); err == nil {
		t.Fatal("2fa started for an unknown admin")
	}
}

// server 007 R4: a code works once; the next step works again; wrong codes fail.
func TestAdmin2FAVerifyOnce(t *testing.T) {
	st, _ := open2FA(t)
	now := time.Unix(1700000000, 0)
	st.Now = func() time.Time { return now }
	secret, _ := st.Admin2FAStart("admin")
	st.Admin2FAConfirm("admin", totpCode(secret, now.Unix()/30))
	now = now.Add(60 * time.Second)
	c := totpCode(secret, now.Unix()/30)
	if err := st.Admin2FAVerify("admin", c); err != nil {
		t.Fatal(err)
	}
	if err := st.Admin2FAVerify("admin", c); !errors.Is(err, ErrBad2FA) {
		t.Fatal("same code accepted twice")
	}
	if err := st.Admin2FAVerify("admin", "111111"); !errors.Is(err, ErrBad2FA) {
		t.Fatal("wrong code accepted")
	}
	now = now.Add(30 * time.Second)
	if err := st.Admin2FAVerify("admin", totpCode(secret, now.Unix()/30)); err != nil {
		t.Fatal("next step refused:", err)
	}
}

// server 007 R3: each backup code works exactly once.
func TestAdmin2FABackupCodes(t *testing.T) {
	st, _ := open2FA(t)
	secret, _ := st.Admin2FAStart("admin")
	codes, _ := st.Admin2FAConfirm("admin", totpCode(secret, st.Now().Unix()/30))
	if err := st.Admin2FAVerify("admin", strings.ToUpper(codes[0])); err != nil {
		t.Fatal("backup code refused:", err)
	}
	if err := st.Admin2FAVerify("admin", codes[0]); !errors.Is(err, ErrBad2FA) {
		t.Fatal("backup code used twice")
	}
	if st.Admin2FARemaining("admin") != 9 {
		t.Fatal("remaining", st.Admin2FARemaining("admin"))
	}
	if err := st.Admin2FAVerify("admin", "aaaaa-aaaaa"); !errors.Is(err, ErrBad2FA) {
		t.Fatal("random backup code accepted")
	}
}

// server 007 R6: disable removes everything and logs the admin out.
func TestAdmin2FADisable(t *testing.T) {
	st, _ := open2FA(t)
	secret, _ := st.Admin2FAStart("admin")
	st.Admin2FAConfirm("admin", totpCode(secret, st.Now().Unix()/30))
	sid, _, _ := st.PanelSessionCreate("admin", "1.1.1.1", time.Hour)
	if err := st.Admin2FADisable("admin"); err != nil {
		t.Fatal(err)
	}
	if st.Admin2FAEnabled("admin") || st.Admin2FARemaining("admin") != 0 {
		t.Fatal("still enabled")
	}
	if _, err := st.PanelSessionGet(sid); err == nil {
		t.Fatal("session survived disable")
	}
	if err := st.Admin2FADisable("admin"); err == nil {
		t.Fatal("disable twice should say nothing was enabled")
	}
}
