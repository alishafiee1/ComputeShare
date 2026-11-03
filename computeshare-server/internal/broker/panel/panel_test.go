package panel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alishafiee1/computeshare-server/internal/broker/store"
)

type fakeBE struct {
	st           *store.Store
	disconnected []string
}

func (f *fakeBE) AdminNodes() []NodeView {
	ns, _ := f.st.Nodes()
	var out []NodeView
	for _, n := range ns {
		out = append(out, NodeView{ID: n.ID, Name: n.Name, Status: n.Status})
	}
	return out
}
func (f *fakeBE) Disconnect(id string) bool       { f.disconnected = append(f.disconnected, id); return true }
func (f *fakeBE) ClientIP(r *http.Request) string { return strings.Split(r.RemoteAddr, ":")[0] }

const goodPW = "Shiraz-GPU-2026!"

func setup(t *testing.T) (*Panel, *store.Store, *fakeBE) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.AdminSetPassword("admin", goodPW); err != nil {
		t.Fatal(err)
	}
	be := &fakeBE{st: st}
	return New(Config{}, st, be), st, be
}

func do(p *Panel, method, path string, form url.Values, cookie *http.Cookie, ip string) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.RemoteAddr = ip + ":1234"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

func login(t *testing.T, p *Panel) (*http.Cookie, string) {
	w := do(p, "POST", "/panel/login", url.Values{"username": {"admin"}, "password": {goodPW}}, nil, "10.0.0.1")
	if w.Code != 303 {
		t.Fatal("login", w.Code, w.Body.String())
	}
	var c *http.Cookie
	for _, x := range w.Result().Cookies() {
		if x.Name == "cs_panel" {
			c = x
		}
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie flags", c)
	}
	page := do(p, "GET", "/panel/", nil, c, "10.0.0.1")
	m := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(page.Body.String())
	if page.Code != 200 || m == nil {
		t.Fatal("nodes page", page.Code)
	}
	return c, m[1]
}

func TestAuthRequiredAndHeaders(t *testing.T) {
	p, _, _ := setup(t)
	w := do(p, "GET", "/panel/", nil, nil, "10.0.0.1")
	if w.Code != 303 || w.Header().Get("Location") != "/panel/login" {
		t.Fatal(w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("security headers missing")
	}
	if do(p, "GET", "/panel/static/app.css", nil, nil, "10.0.0.1").Code != 200 {
		t.Fatal("static")
	}
}

func TestLoginRateLimitAndAudit(t *testing.T) {
	p, st, _ := setup(t)
	for i := 0; i < 5; i++ {
		if w := do(p, "POST", "/panel/login", url.Values{"username": {"admin"}, "password": {"wrong"}}, nil, "6.6.6.6"); w.Code != 401 {
			t.Fatal(i, w.Code)
		}
	}
	// locked even with the right password, from that IP
	if w := do(p, "POST", "/panel/login", url.Values{"username": {"admin"}, "password": {goodPW}}, nil, "6.6.6.6"); w.Code != 429 {
		t.Fatal("not locked", w.Code)
	}
	// another IP still works (per-user limit is 10)
	login(t, p)
	a, _ := st.AuditList(50)
	var fails, locked, ok int
	for _, e := range a {
		switch e.Action {
		case "login.fail":
			fails++
		case "login.locked":
			locked++
		case "login.ok":
			ok++
		}
		if strings.Contains(e.Detail, goodPW) {
			t.Fatal("password in audit")
		}
	}
	if fails != 5 || locked != 1 || ok != 1 {
		t.Fatal(fails, locked, ok)
	}
}

func TestCSRFAndOrigin(t *testing.T) {
	p, _, _ := setup(t)
	c, csrf := login(t, p)
	if w := do(p, "POST", "/panel/tokens", url.Values{"label": {"x"}}, c, "10.0.0.1"); w.Code != 403 {
		t.Fatal("missing csrf accepted", w.Code)
	}
	r := httptest.NewRequest("POST", "/panel/tokens", strings.NewReader(url.Values{"label": {"x"}, "csrf": {csrf}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(c)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin accepted", w.Code)
	}
}

func TestTokensApproveLogout(t *testing.T) {
	p, st, be := setup(t)
	c, csrf := login(t, p)
	w := do(p, "POST", "/panel/tokens", url.Values{"label": {"family"}, "ttl_hours": {"24"}, "uses": {"1"}, "csrf": {csrf}}, c, "10.0.0.1")
	tok := regexp.MustCompile(`cs1\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`).FindString(w.Body.String())
	if w.Code != 200 || tok == "" {
		t.Fatal("token not shown", w.Code)
	}
	if strings.Contains(do(p, "GET", "/panel/tokens", nil, c, "10.0.0.1").Body.String(), tok) {
		t.Fatal("token shown twice")
	}
	a, _ := st.AuditList(50)
	for _, e := range a {
		if strings.Contains(e.Detail, tok) {
			t.Fatal("token secret in audit")
		}
	}
	// pending node approval
	st.RequireApproval = true
	n, err := st.Register(testReq(t, "pendingPc", tok))
	if err != nil || n.Status != "pending" {
		t.Fatal(err)
	}
	if !strings.Contains(do(p, "GET", "/panel/", nil, c, "10.0.0.1").Body.String(), "/approve") {
		t.Fatal("approve button missing")
	}
	if w := do(p, "POST", "/panel/nodes/"+n.ID+"/approve", url.Values{"csrf": {csrf}}, c, "10.0.0.1"); w.Code != 303 {
		t.Fatal("approve", w.Code)
	}
	if got, _ := st.NodeByID(n.ID); got.Status != "offline" {
		t.Fatal(got.Status)
	}
	do(p, "POST", "/panel/nodes/"+n.ID+"/ban", url.Values{"csrf": {csrf}}, c, "10.0.0.1")
	if got, _ := st.NodeByID(n.ID); got.Status != "banned" || len(be.disconnected) != 1 {
		t.Fatal("ban", got.Status)
	}
	for _, path := range []string{"/panel/logs", "/panel/audit"} {
		if do(p, "GET", path, nil, c, "10.0.0.1").Code != 200 {
			t.Fatal(path)
		}
	}
	do(p, "POST", "/panel/logout", url.Values{"csrf": {csrf}}, c, "10.0.0.1")
	if w := do(p, "GET", "/panel/", nil, c, "10.0.0.1"); w.Code != 303 {
		t.Fatal("session survived logout")
	}
}

func TestValidatePassword(t *testing.T) {
	for pw, ok := range map[string]bool{goodPW: true, "short1A!": false, "alllowercaseletters": false, "admin-Password-1": false, "correct horse Battery": true} {
		if (ValidatePassword("admin", pw) == nil) != ok {
			t.Errorf("%q", pw)
		}
	}
}
