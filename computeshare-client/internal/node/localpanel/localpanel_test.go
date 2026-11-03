package localpanel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/control"
	"github.com/alishafiee1/computeshare-client/internal/node/stream"
)

type fake struct {
	share  bool
	kicked []uint32
	lim    control.Limits
}

func (f *fake) Status() Status {
	return Status{Name: "myBrotherPc", Server: "https://node.n3xus.ir", JumpHost: "node.n3xus.ir", User: "user", Tunnel: true, Sharing: f.share}
}
func (f *fake) Sessions() []stream.Session {
	return []stream.Session{{ID: 7, SrcIP: "5.6.7.8", Via: "jump", Started: time.Now()}}
}
func (f *fake) Kick(id uint32) bool    { f.kicked = append(f.kicked, id); return id == 7 }
func (f *fake) KickIP(ip string) int   { return 1 }
func (f *fake) KickAll() int           { return 1 }
func (f *fake) SetShare(on bool) error { f.share = on; return nil }
func (f *fake) ApplyLimits(l control.Limits) error {
	f.lim = l
	return l.Validate()
}

const tok = "0123456789abcdef0123456789abcdef0123456789abcdef"

func req(p *Panel, method, path, host string, form url.Values, c *http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Host = host
	if c != nil {
		r.AddCookie(c)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

func TestLocalPanel(t *testing.T) {
	f := &fake{share: true}
	p := New(f, DefaultAddr, tok)
	h := "127.0.0.1:7777"
	if w := req(p, "GET", "/", h, nil, nil, nil); w.Code != 401 {
		t.Fatal("no auth", w.Code)
	}
	if w := req(p, "GET", "/?t="+tok, "evil.example:7777", nil, nil, nil); w.Code != 403 {
		t.Fatal("dns rebinding host accepted", w.Code)
	}
	if w := req(p, "GET", "/?t=wrong", h, nil, nil, nil); w.Code != 401 {
		t.Fatal("wrong token", w.Code)
	}
	w := req(p, "GET", "/?t="+tok, h, nil, nil, nil)
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal(w.Code)
	}
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie flags")
	}
	w = req(p, "GET", "/", h, nil, c, nil)
	body := w.Body.String()
	for _, s := range []string{"وصل", "روشن", "5.6.7.8", "ssh -J join@node.n3xus.ir:2222 user@myBrotherPc"} {
		if !strings.Contains(body, s) {
			t.Fatalf("missing %q", s)
		}
	}
	csrf := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)[1]
	if w := req(p, "POST", "/kick", h, url.Values{"id": {"7"}}, c, nil); w.Code != 403 {
		t.Fatal("csrf missing accepted")
	}
	if w := req(p, "POST", "/kick", h, url.Values{"id": {"7"}, "csrf": {csrf}}, c, map[string]string{"Origin": "http://evil.example"}); w.Code != 403 {
		t.Fatal("cross-origin accepted")
	}
	if w := req(p, "POST", "/kick", h, url.Values{"id": {"7"}, "csrf": {csrf}}, c, nil); w.Code != 303 || len(f.kicked) != 1 {
		t.Fatal("kick", w.Code)
	}
	req(p, "POST", "/share", h, url.Values{"on": {"0"}, "csrf": {csrf}}, c, nil)
	if f.share {
		t.Fatal("share not off")
	}
	w = req(p, "POST", "/limits", h, url.Values{"cpu": {"x"}, "csrf": {csrf}}, c, nil)
	if !strings.Contains(w.Header().Get("Location"), "err=") {
		t.Fatal("invalid limit accepted")
	}
	req(p, "POST", "/limits", h, url.Values{"bandwidth": {"20Mbit"}, "quota": {""}, "csrf": {csrf}}, c, nil)
	if f.lim.Bandwidth == nil || *f.lim.Bandwidth != "20Mbit" || f.lim.Quota == nil || *f.lim.Quota != "" || f.lim.CPU != nil {
		t.Fatal("partial limits", f.lim)
	}
	// CLI bearer access
	if w := req(p, "POST", "/kick", h, url.Values{"ip": {"5.6.7.8"}}, nil, map[string]string{"Authorization": "Bearer " + tok}); w.Code != 200 {
		t.Fatal("bearer", w.Code, w.Body.String())
	}
}

func TestLoopbackAndToken(t *testing.T) {
	for a, ok := range map[string]bool{"127.0.0.1:7777": true, "[::1]:7777": true, "localhost:7777": true, "0.0.0.0:7777": false, "192.168.1.5:7777": false} {
		if (CheckLoopback(a) == nil) != ok {
			t.Error(a)
		}
	}
	p := filepath.Join(t.TempDir(), "panel-token")
	a, _ := LoadOrCreateToken(p)
	b, _ := LoadOrCreateToken(p)
	if a != b || len(a) < 32 {
		t.Fatal("token")
	}
}

type fakeUsers struct {
	fake
	disabled map[string]bool
}

func (f *fakeUsers) Users() []UserRow {
	return []UserRow{{Name: "ali", State: "running", LastLogin: "2026-10-30 10:00", Disk: "1.2 GB / 20GB", Sessions: 2, Disabled: f.disabled["ali"]}}
}
func (f *fakeUsers) SetUserDisabled(n string, d bool) error { f.disabled[n] = d; return nil }

// client 015 R12: the Users table shows only in per-user mode and can disable a user.
func TestUsersTable(t *testing.T) {
	h := "127.0.0.1:7777"
	plain := New(&fake{}, DefaultAddr, tok)
	c := req(plain, "GET", "/?t="+tok, h, nil, nil, nil).Result().Cookies()[0]
	if strings.Contains(req(plain, "GET", "/", h, nil, c, nil).Body.String(), "<h2>کاربران</h2>") {
		t.Fatal("users table without per-user mode")
	}
	f := &fakeUsers{disabled: map[string]bool{}}
	p := New(f, DefaultAddr, tok)
	c = req(p, "GET", "/?t="+tok, h, nil, nil, nil).Result().Cookies()[0]
	body := req(p, "GET", "/", h, nil, c, nil).Body.String()
	if !strings.Contains(body, "<h2>کاربران</h2>") || !strings.Contains(body, "1.2 GB / 20GB") {
		t.Fatal(body)
	}
	csrf := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)[1]
	if w := req(p, "POST", "/users", h, url.Values{"name": {"ali"}, "disabled": {"1"}, "csrf": {csrf}}, c, nil); w.Code != 303 || !f.disabled["ali"] {
		t.Fatal("disable", w.Code)
	}
}
