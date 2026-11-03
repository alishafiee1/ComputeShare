// Package panel is the server admin web panel at /panel/ (server card 003).
// Go html/template + embedded assets + a few lines of JS; no external resources.
package panel

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/alishafiee1/computeshare-server/internal/broker/store"
)

//go:embed assets/*
var assets embed.FS

// NodeView is one node as the admin sees it (pending nodes included).
type NodeView struct {
	ID, Name, Status, OS, Arch, Version string
	Specs                               proto.Specs
	Limits                              proto.Limits
	LoadNow, Avg24h, RAMPct, GPUPct     *float64
	LastSeen, UptimeS, RegisteredAt     int64
	HostKeyFP                           string
}

// Backend is implemented by the broker server.
type Backend interface {
	AdminNodes() []NodeView
	Disconnect(nodeID string) bool
	ClientIP(r *http.Request) string
}

type Config struct {
	// SecureCookie sets Secure + __Host- prefix (production behind TLS). Lab over plain HTTP: false.
	SecureCookie bool
	SessionTTL   time.Duration // default 12h
	// Login limits: per IP and per username, within Window.
	MaxFailIP, MaxFailUser int
	Window                 time.Duration
}

type Panel struct {
	cfg  Config
	st   *store.Store
	be   Backend
	tpl  *template.Template
	mux  *http.ServeMux
	mu   sync.Mutex
	fail map[string][]time.Time
	Now  func() time.Time
	// pending holds logins that passed the password and wait for the second step (server card 007).
	pending map[string]*pendingLogin
}

// pendingLogin is a password-checked login waiting for a 2FA code; it lives 5 minutes and allows 5 tries.
type pendingLogin struct {
	user, ip string
	exp      time.Time
	tries    int
}

const (
	pendingTTL   = 5 * time.Minute
	pendingTries = 5
)

func New(cfg Config, st *store.Store, be Backend) *Panel {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if cfg.MaxFailIP == 0 {
		cfg.MaxFailIP = 5
	}
	if cfg.MaxFailUser == 0 {
		cfg.MaxFailUser = 10
	}
	if cfg.Window == 0 {
		cfg.Window = 15 * time.Minute
	}
	p := &Panel{cfg: cfg, st: st, be: be, fail: map[string][]time.Time{}, Now: time.Now, pending: map[string]*pendingLogin{}}
	p.tpl = template.Must(template.New("").Funcs(template.FuncMap{
		"pct": pct,
		"ts": func(t int64) string {
			if t == 0 {
				return "-"
			}
			return time.Unix(t, 0).UTC().Format("2006-01-02 15:04:05Z")
		},
		"dur":   dur,
		"bytes": human,
		"orunl": func(s string) string {
			if s == "" {
				return "unlimited"
			}
			return s
		},
	}).ParseFS(assets, "assets/*.html"))
	m := http.NewServeMux()
	static, _ := fs.Sub(assets, "assets")
	m.Handle("GET /panel/static/", http.StripPrefix("/panel/static/", http.FileServer(http.FS(static))))
	m.HandleFunc("GET /panel/login", p.loginForm)
	m.HandleFunc("POST /panel/login", p.login)
	m.HandleFunc("POST /panel/login/2fa", p.login2FA)
	m.HandleFunc("POST /panel/logout", p.auth(p.logout))
	m.HandleFunc("GET /panel/{$}", p.auth(p.nodes))
	m.HandleFunc("POST /panel/nodes/{id}/{action}", p.auth(p.nodeAction))
	m.HandleFunc("GET /panel/tokens", p.auth(p.tokens))
	m.HandleFunc("POST /panel/tokens", p.auth(p.tokenCreate))
	m.HandleFunc("POST /panel/tokens/{id}/revoke", p.auth(p.tokenRevoke))
	m.HandleFunc("GET /panel/logs", p.auth(p.logs))
	m.HandleFunc("GET /panel/audit", p.auth(p.audit))
	p.mux = m
	return p
}

func (p *Panel) cookieName() string {
	if p.cfg.SecureCookie {
		return "__Host-cs_panel"
	}
	return "cs_panel"
}

func (p *Panel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost && !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	p.mux.ServeHTTP(w, r)
}

// sameOrigin rejects POSTs whose Origin (or Referer) names another host.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		o = r.Header.Get("Referer")
	}
	if o == "" || o == "null" {
		return o == "" // no header: older clients; CSRF token still required
	}
	u, err := url.Parse(o)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

type ctxSess struct {
	*store.PanelSession
	sid string
}

type page struct {
	Title, User, CSRF, Flash, Error string
	Data                            any
}

func (p *Panel) render(w http.ResponseWriter, code int, name string, pg page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := p.tpl.ExecuteTemplate(w, name, pg); err != nil {
		fmt.Fprint(w, "template error")
	}
}

type handler func(w http.ResponseWriter, r *http.Request, s *ctxSess)

func (p *Panel) auth(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(p.cookieName())
		if err != nil {
			http.Redirect(w, r, "/panel/login", http.StatusSeeOther)
			return
		}
		ps, err := p.st.PanelSessionGet(c.Value)
		if err != nil {
			http.Redirect(w, r, "/panel/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost && subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(ps.CSRF)) != 1 {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		h(w, r, &ctxSess{ps, c.Value})
	}
}

// ---- login with rate limit ----

func (p *Panel) recent(key string) int {
	cut := p.Now().Add(-p.cfg.Window)
	l := p.fail[key][:0]
	for _, t := range p.fail[key] {
		if t.After(cut) {
			l = append(l, t)
		}
	}
	if len(l) == 0 {
		delete(p.fail, key)
	} else {
		p.fail[key] = l
	}
	return len(l)
}

// Locked reports whether logins from ip or for user are temporarily refused.
func (p *Panel) Locked(ip, user string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.recent("ip:"+ip) >= p.cfg.MaxFailIP || p.recent("user:"+user) >= p.cfg.MaxFailUser
}

func (p *Panel) addFail(ip, user string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.Now()
	p.fail["ip:"+ip] = append(p.fail["ip:"+ip], now)
	p.fail["user:"+user] = append(p.fail["user:"+user], now)
}

func (p *Panel) loginForm(w http.ResponseWriter, r *http.Request) {
	msg := ""
	if p.st.AdminCount() == 0 {
		msg = "No admin account yet. On the server run: sudo -u csbroker csbroker admin set-password --user admin"
	}
	p.render(w, 200, "login.html", page{Title: "Sign in", Error: msg})
}

func (p *Panel) login(w http.ResponseWriter, r *http.Request) {
	ip := p.be.ClientIP(r)
	user := strings.TrimSpace(r.PostFormValue("username"))
	if len(user) > 64 {
		user = user[:64]
	}
	if p.Locked(ip, user) {
		p.st.Audit(user, ip, "login.locked", "")
		p.render(w, http.StatusTooManyRequests, "login.html", page{Title: "Sign in", Error: "Too many failed attempts. Try again later."})
		return
	}
	if err := p.st.AdminCheck(user, r.PostFormValue("password")); err != nil {
		p.addFail(ip, user)
		p.st.Audit(user, ip, "login.fail", "")
		p.render(w, http.StatusUnauthorized, "login.html", page{Title: "Sign in", Error: "Invalid username or password."})
		return
	}
	if p.st.Admin2FAEnabled(user) {
		// password is right: ask for the second step before any session exists (card 007 R4)
		tok := p.newPending(user, ip)
		p.st.Audit(user, ip, "login.2fa.required", "")
		p.render(w, 200, "login2fa.html", page{Title: "Second step", Data: tok})
		return
	}
	p.finishLogin(w, r, user, ip, "")
}

// finishLogin creates the session and the cookie. detail goes to the audit log.
func (p *Panel) finishLogin(w http.ResponseWriter, r *http.Request, user, ip, detail string) {
	sid, _, err := p.st.PanelSessionCreate(user, ip, p.cfg.SessionTTL)
	if err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	p.st.Audit(user, ip, "login.ok", detail)
	http.SetCookie(w, &http.Cookie{Name: p.cookieName(), Value: sid, Path: "/", HttpOnly: true, Secure: p.cfg.SecureCookie,
		SameSite: http.SameSiteStrictMode, MaxAge: int(p.cfg.SessionTTL / time.Second)})
	http.Redirect(w, r, "/panel/", http.StatusSeeOther)
}

func (p *Panel) newPending(user, ip string) string {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.Now()
	for k, v := range p.pending { // drop expired entries
		if now.After(v.exp) {
			delete(p.pending, k)
		}
	}
	p.pending[tok] = &pendingLogin{user: user, ip: ip, exp: now.Add(pendingTTL)}
	return tok
}

// login2FA is the second step: a TOTP or backup code for a login that already passed the password.
func (p *Panel) login2FA(w http.ResponseWriter, r *http.Request) {
	ip := p.be.ClientIP(r)
	tok := r.PostFormValue("token")
	p.mu.Lock()
	pl := p.pending[tok]
	if pl != nil && (p.Now().After(pl.exp) || pl.ip != ip) {
		delete(p.pending, tok)
		pl = nil
	}
	p.mu.Unlock()
	if pl == nil {
		p.render(w, http.StatusUnauthorized, "login.html", page{Title: "Sign in", Error: "The sign-in expired. Enter your password again."})
		return
	}
	if p.Locked(ip, pl.user) {
		p.st.Audit(pl.user, ip, "login.locked", "2fa")
		p.render(w, http.StatusTooManyRequests, "login.html", page{Title: "Sign in", Error: "Too many failed attempts. Try again later."})
		return
	}
	if err := p.st.Admin2FAVerify(pl.user, r.PostFormValue("code")); err != nil {
		p.addFail(ip, pl.user) // the same limits as password failures
		p.st.Audit(pl.user, ip, "login.2fa.fail", "")
		p.mu.Lock()
		pl.tries++
		if pl.tries >= pendingTries {
			delete(p.pending, tok)
		}
		gone := pl.tries >= pendingTries
		p.mu.Unlock()
		if gone {
			p.render(w, http.StatusUnauthorized, "login.html", page{Title: "Sign in", Error: "Too many wrong codes. Enter your password again."})
			return
		}
		p.render(w, http.StatusUnauthorized, "login2fa.html", page{Title: "Second step", Data: tok, Error: "That code is not valid."})
		return
	}
	p.mu.Lock()
	delete(p.pending, tok)
	p.mu.Unlock()
	p.finishLogin(w, r, pl.user, ip, "2fa")
}

func (p *Panel) logout(w http.ResponseWriter, r *http.Request, s *ctxSess) {
	p.st.PanelSessionDelete(s.sid)
	p.st.Audit(s.Username, p.be.ClientIP(r), "logout", "")
	http.SetCookie(w, &http.Cookie{Name: p.cookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: p.cfg.SecureCookie, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/panel/login", http.StatusSeeOther)
}

// ---- pages ----

func (p *Panel) pg(s *ctxSess, title string, data any) page {
	return page{Title: title, User: s.Username, CSRF: s.CSRF, Data: data}
}

func (p *Panel) nodes(w http.ResponseWriter, r *http.Request, s *ctxSess) {
	pg := p.pg(s, "Nodes", p.be.AdminNodes())
	pg.Flash = r.URL.Query().Get("ok")
	p.render(w, 200, "nodes.html", pg)
}

func (p *Panel) nodeAction(w http.ResponseWriter, r *http.Request, s *ctxSess) {
	ip := p.be.ClientIP(r)
	n, err := p.st.NodeByID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "no such node", 404)
		return
	}
	act := r.PathValue("action")
	switch act {
	case "approve":
		err = p.st.Approve(n.ID)
	case "ban":
		p.st.BanNode(n, "banned from panel by "+s.Username)
		p.be.Disconnect(n.ID)
	case "unban":
		p.st.UnbanNode(n.Name)
	case "disconnect":
		if !p.be.Disconnect(n.ID) {
			err = errors.New("node is not connected")
		}
	case "release":
		p.be.Disconnect(n.ID)
		err = p.st.Release(n.ID)
	default:
		http.Error(w, "unknown action", 400)
		return
	}
	if err != nil {
		p.render(w, 409, "nodes.html", page{Title: "Nodes", User: s.Username, CSRF: s.CSRF, Error: act + " " + n.Name + ": " + err.Error(), Data: p.be.AdminNodes()})
		return
	}
	p.st.Audit(s.Username, ip, "node."+act, n.Name)
	http.Redirect(w, r, "/panel/?ok="+url.QueryEscape(act+" "+n.Name), http.StatusSeeOther)
}

type tokensData struct {
	Tokens   []store.TokenRow
	NewToken string
}

func (p *Panel) tokens(w http.ResponseWriter, r *http.Request, s *ctxSess) {
	ts, _ := p.st.TokenList()
	p.render(w, 200, "tokens.html", p.pg(s, "Join tokens", tokensData{Tokens: ts}))
}

func (p *Panel) tokenCreate(w http.ResponseWriter, r *http.Request, s *ctxSess) {
	label := strings.TrimSpace(r.PostFormValue("label"))
	ttlH, _ := strconv.Atoi(r.PostFormValue("ttl_hours"))
	uses, _ := strconv.Atoi(r.PostFormValue("uses"))
	if len(label) > 64 || ttlH < 0 || uses < 0 {
		http.Error(w, "invalid input", 400)
		return
	}
	id, tok, err := p.st.TokenCreate(label, time.Duration(ttlH)*time.Hour, uses)
	if err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	p.st.Audit(s.Username, p.be.ClientIP(r), "token.create", id+" "+label) // never the secret
	ts, _ := p.st.TokenList()
	pg := p.pg(s, "Join tokens", tokensData{Tokens: ts, NewToken: tok})
	pg.Flash = "Token " + id + " created. Copy it now; it is shown only once."
	p.render(w, 200, "tokens.html", pg)
}

func (p *Panel) tokenRevoke(w http.ResponseWriter, r *http.Request, s *ctxSess) {
	id := r.PathValue("id")
	if err := p.st.TokenRevoke(id); err != nil {
		http.Error(w, "no such token", 404)
		return
	}
	p.st.Audit(s.Username, p.be.ClientIP(r), "token.revoke", id)
	http.Redirect(w, r, "/panel/tokens", http.StatusSeeOther)
}

func (p *Panel) logs(w http.ResponseWriter, r *http.Request, s *ctxSess) {
	l, _ := p.st.RecentSessions(300)
	p.render(w, 200, "logs.html", p.pg(s, "Connection log", l))
}

func (p *Panel) audit(w http.ResponseWriter, r *http.Request, s *ctxSess) {
	l, _ := p.st.AuditList(300)
	p.render(w, 200, "audit.html", p.pg(s, "Audit log", l))
}

// ---- helpers ----

// ValidatePassword enforces the admin password policy: at least 12 characters,
// three of four classes (lower, upper, digit, symbol), and not containing the username.
func ValidatePassword(user, pw string) error {
	if len([]rune(pw)) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if len(pw) > 72 {
		return errors.New("password must be at most 72 bytes")
	}
	var lo, up, di, sy int
	for _, c := range pw {
		switch {
		case unicode.IsLower(c):
			lo = 1
		case unicode.IsUpper(c):
			up = 1
		case unicode.IsDigit(c):
			di = 1
		default:
			sy = 1
		}
	}
	if lo+up+di+sy < 3 {
		return errors.New("password needs three of: lowercase, uppercase, digit, symbol")
	}
	if user != "" && strings.Contains(strings.ToLower(pw), strings.ToLower(user)) {
		return errors.New("password must not contain the username")
	}
	return nil
}

func pct(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", *p)
}

func dur(s int64) string {
	if s <= 0 {
		return "-"
	}
	d := time.Duration(s) * time.Second
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	return d.Truncate(time.Minute).String()
}

func human(b int64) string {
	const k = 1024
	switch {
	case b >= k*k*k:
		return fmt.Sprintf("%.1f GiB", float64(b)/(k*k*k))
	case b >= k*k:
		return fmt.Sprintf("%.1f MiB", float64(b)/(k*k))
	case b >= k:
		return fmt.Sprintf("%.1f KiB", float64(b)/k)
	}
	return fmt.Sprintf("%d B", b)
}
