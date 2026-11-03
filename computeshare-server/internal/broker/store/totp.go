package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; authenticator apps expect HMAC-SHA1
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238): 6 digits, 30 s step, HMAC-SHA1, secret of 160 bits. Used for the admin
// panel's second login step (server card 007). No third-party code: the formula is small.

const (
	totpStep   = 30
	totpDigits = 6
	// SecretBytes is the size of a new secret: 160 bits, as RFC 4226 recommends.
	SecretBytes = 20
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a fresh random secret.
func NewTOTPSecret() []byte {
	b := make([]byte, SecretBytes)
	rand.Read(b)
	return b
}

// EncodeSecret is the base32 text a user types into an authenticator app.
func EncodeSecret(secret []byte) string { return b32.EncodeToString(secret) }

// TOTPURI is the otpauth:// address an authenticator app can import (also what a QR code holds).
func TOTPURI(issuer, user string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + user)
	q := url.Values{"secret": {EncodeSecret(secret)}, "issuer": {issuer}, "algorithm": {"SHA1"},
		"digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(totpStep)}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// totpCode is the code for one time step.
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, v%mod)
}

// TOTPCodeAt is the code an authenticator app shows at time t (used by tests and tooling).
func TOTPCodeAt(secret []byte, t time.Time) string { return totpCode(secret, t.Unix()/totpStep) }

// verifyTOTP accepts the current step and one step either side (clock drift). A step at or
// below last is refused, so a code cannot be used twice. It returns the step that matched.
func verifyTOTP(secret []byte, code string, now time.Time, last int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	cur := now.Unix() / totpStep
	for _, st := range []int64{cur - 1, cur, cur + 1} {
		if st > last && hmac.Equal([]byte(totpCode(secret, st)), []byte(code)) {
			return st, true
		}
	}
	return 0, false
}
