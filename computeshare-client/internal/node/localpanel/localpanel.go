// Package localpanel is the owner's web panel on the node computer (client card 007).
// It listens on loopback only; access needs a random token kept in a 0600 file, then a
// SameSite=Strict cookie. Every change needs the CSRF token; Host and Origin are checked
// (DNS-rebinding and cross-site protection).
package localpanel

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/control"
	"github.com/alishafiee1/computeshare-client/internal/node/stream"
)

//go:embed assets/*
var assets embed.FS

// DefaultAddr is the default listen address.
const DefaultAddr = "127.0.0.1:7777"

// Status is what the panel shows at the top.
type Status struct {
	Name, Server, JumpHost, User string
	Registered, Tunnel, Sharing  bool
	Space, SpaceIP, HostKeyFP    string
	CPU, Memory, Bandwidth       string
	Quota                        string
	UsedTodayGB                  float64
	QuotaReached                 bool
}

// Backend is implemented by the csnode agent.
type Backend interface {
	Status() Status
	Sessions() []stream.Session
	Kick(id uint32) bool
	KickIP(ip string) int
	KickAll() int
	SetShare(on bool) error
	ApplyLimits(l control.Limits) error
}

// UserRow is one line of the Users table (client 015 R12).
type UserRow struct {
	Name, State, LastLogin, Disk string
	Sessions                     int
	Disabled                     bool
}

// UsersBackend is implemented by agents with per-user spaces; the table shows only then.
type UsersBackend interface {
	Users() []UserRow
	SetUserDisabled(name string, disabled bool) error
}

type Panel struct {
	be    Backend
	token string
	csrf  string
	addr  string
	tpl   *template.Template
	mux   *http.ServeMux
}

// CheckLoopback refuses any non-loopback listen address.
func CheckLoopback(addr string) error {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if h == "localhost" {
		return nil
	}
	ip := net.ParseIP(h)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("local panel must listen on 127.0.0.1 or ::1 only")
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// LoadOrCreateToken reads the access token file (mode 0600) or creates it.
func LoadOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	t := randHex(24)
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}

func New(be Backend, addr, token string) *Panel {
	p := &Panel{be: be, token: token, csrf: randHex(16), addr: addr}
	p.tpl = template.Must(template.New("").Funcs(template.FuncMap{
		"since": func(t time.Time) string { return time.Since(t).Truncate(time.Second).String() },
		"orunl": func(s string) string {
			if s == "" {
				return "unlimited"
			}
			return s
		},
	}).ParseFS(assets, "assets/*.html"))
	m := http.NewServeMux()
	static, _ := fs.Sub(assets, "assets")
	m.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	m.HandleFunc("GET /{$}", p.auth(p.index))
	m.HandleFunc("POST /share", p.auth(p.share))
	m.HandleFunc("POST /limits", p.auth(p.limits))
	m.HandleFunc("POST /kick", p.auth(p.kick))
	m.HandleFunc("POST /users", p.auth(p.users))
	m.HandleFunc("GET /api/status", p.auth(p.apiStatus))
	p.mux = m
	return p
}

// URL is the one-time login URL printed by `csnode panel`.
func URL(addr, token string) string { return "http://" + addr + "/?t=" + token }

func (p *Panel) hostOK(host string) bool {
	_, port, _ := net.SplitHostPort(p.addr)
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]"} {
		if strings.EqualFold(host, h+":"+port) {
			return true
		}
	}
	return false
}

func (p *Panel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.hostOK(r.Host) {
		http.Error(w, "forbidden host", http.StatusForbidden) // DNS rebinding guard
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost {
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || !p.hostOK(u.Host) {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
	}
	p.mux.ServeHTTP(w, r)
}

func (p *Panel) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("t"); t != "" && r.Method == http.MethodGet {
			if subtle.ConstantTimeCompare([]byte(t), []byte(p.token)) == 1 {
				http.SetCookie(w, &http.Cookie{Name: "csnode_panel", Value: p.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
				http.Redirect(w, r, "/", http.StatusSeeOther) // drop the token from the address bar
				return
			}
		}
		ok := false
		if c, err := r.Cookie("csnode_panel"); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(p.token)) == 1 {
			ok = true
		}
		// CLI access (csnode kick/share): bearer token from the 0600 file
		if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") && subtle.ConstantTimeCompare([]byte(a[7:]), []byte(p.token)) == 1 {
			ok = true
			r.Header.Set("X-CSRF", p.csrf)
		}
		if !ok {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintln(w, "Open the panel with the link printed by: csnode panel")
			return
		}
		if r.Method == http.MethodPost {
			v := r.PostFormValue("csrf")
			if v == "" {
				v = r.Header.Get("X-CSRF")
			}
			if subtle.ConstantTimeCompare([]byte(v), []byte(p.csrf)) != 1 {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		h(w, r)
	}
}

type view struct {
	S        Status
	Sessions []stream.Session
	Users    []UserRow
	PerUser  bool
	CSRF     string
	Flash    string
	Error    string
}

func (p *Panel) index(w http.ResponseWriter, r *http.Request) {
	p.page(w, 200, r.URL.Query().Get("ok"), r.URL.Query().Get("err"))
}

func (p *Panel) page(w http.ResponseWriter, code int, flash, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	v := view{S: p.be.Status(), Sessions: p.be.Sessions(), CSRF: p.csrf, Flash: flash, Error: errMsg}
	if ub, ok := p.be.(UsersBackend); ok {
		v.PerUser, v.Users = true, ub.Users()
	}
	p.tpl.ExecuteTemplate(w, "index.html", v)
}

func back(w http.ResponseWriter, r *http.Request, ok, errMsg string) {
	if r.Header.Get("Authorization") != "" { // CLI
		if errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, ok)
		return
	}
	q := url.Values{}
	if ok != "" {
		q.Set("ok", ok)
	}
	if errMsg != "" {
		q.Set("err", errMsg)
	}
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusSeeOther)
}

func (p *Panel) share(w http.ResponseWriter, r *http.Request) {
	on := r.PostFormValue("on") == "1"
	if err := p.be.SetShare(on); err != nil {
		back(w, r, "", err.Error())
		return
	}
	msg := "Sharing is on: new connections are accepted."
	if !on {
		n := p.be.KickAll()
		msg = fmt.Sprintf("Sharing is off: new connections are refused; %d live session(s) closed.", n)
	}
	back(w, r, msg, "")
}

func (p *Panel) limits(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var l control.Limits
	for k, dst := range map[string]**string{"cpu": &l.CPU, "memory": &l.Memory, "bandwidth": &l.Bandwidth, "quota": &l.Quota} {
		if vs, ok := r.PostForm[k]; ok {
			v := vs[0]
			*dst = &v
		}
	}
	if err := p.be.ApplyLimits(l); err != nil {
		back(w, r, "", err.Error())
		return
	}
	back(w, r, "Limits saved and applied.", "")
}

func (p *Panel) kick(w http.ResponseWriter, r *http.Request) {
	if ip := r.PostFormValue("ip"); ip != "" {
		back(w, r, fmt.Sprintf("Disconnected %d session(s) from %s.", p.be.KickIP(ip), ip), "")
		return
	}
	id, err := strconv.ParseUint(r.PostFormValue("id"), 10, 32)
	if err != nil || !p.be.Kick(uint32(id)) {
		back(w, r, "", "no such session")
		return
	}
	back(w, r, "Session disconnected.", "")
}

func (p *Panel) users(w http.ResponseWriter, r *http.Request) {
	ub, ok := p.be.(UsersBackend)
	if !ok {
		back(w, r, "", "per-user spaces are off on this node")
		return
	}
	name, dis := r.PostFormValue("name"), r.PostFormValue("disabled") == "1"
	if err := ub.SetUserDisabled(name, dis); err != nil {
		back(w, r, "", err.Error())
		return
	}
	if dis {
		back(w, r, fmt.Sprintf("%s is disabled; their space was stopped.", name), "")
		return
	}
	back(w, r, fmt.Sprintf("%s can log in again.", name), "")
}

func (p *Panel) apiStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"tunnel":%v,"sharing":%v,"sessions":%d}`+"\n", p.be.Status().Tunnel, p.be.Status().Sharing, len(p.be.Sessions()))
}
