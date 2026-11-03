package netutil

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	tr, err := ParseTrusted("127.0.0.1, 172.18.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		remote, xff, xreal, want string
		trusted                  Trusted
	}{
		{"203.0.113.9:5555", "1.2.3.4", "", "203.0.113.9", tr},                // untrusted peer: header ignored
		{"172.18.0.2:40000", "198.51.100.7", "", "198.51.100.7", tr},          // nginx in docker
		{"172.18.0.2:40000", "6.6.6.6, 198.51.100.7", "", "198.51.100.7", tr}, // spoofed left part ignored
		{"127.0.0.1:1", "198.51.100.7, 172.18.0.5", "", "198.51.100.7", tr},   // chain of trusted proxies
		{"127.0.0.1:1", "", "198.51.100.8", "198.51.100.8", tr},               // X-Real-IP fallback
		{"127.0.0.1:1", "garbage", "", "127.0.0.1", tr},                       // malformed
		{"127.0.0.1:1", "198.51.100.7", "", "127.0.0.1", nil},                 // nothing trusted
	}
	for i, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.xreal != "" {
			r.Header.Set("X-Real-IP", c.xreal)
		}
		if got := c.trusted.ClientIP(r); got != c.want {
			t.Errorf("case %d: got %s want %s", i, got, c.want)
		}
	}
	if _, err := ParseTrusted("nope"); err == nil {
		t.Fatal("invalid accepted")
	}
}
