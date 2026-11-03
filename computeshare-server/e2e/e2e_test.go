package e2e

// In-process end-to-end tests for computeshare-server: a real csbroker (HTTP API, WSS tunnel,
// SSH jump on one port, admin panel) with fake nodes whose streams reach an in-process sshd
// ("the space"). Scenario names describe what each test proves.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/alishafiee1/computeshare-server/internal/broker/netutil"
	"github.com/alishafiee1/computeshare-server/internal/broker/panel"
	"github.com/alishafiee1/computeshare-server/internal/broker/server"
	"github.com/alishafiee1/computeshare-server/internal/broker/store"
	"golang.org/x/crypto/ssh"
)

const spacePW = "Shiraz-2026-gpu"

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b.Bytes()...)
}

// fakeSpace is an sshd standing in for the node's Incus space: user "user" + password,
// exec "hostname" -> "cs-<name>", exec "echo X" -> "X".
func fakeSpace(t *testing.T, name string) (addr string, hostKey ssh.PublicKey) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "user" && string(pw) == spacePW {
			return nil, nil
		}
		return nil, fmt.Errorf("denied")
	}}
	cfg.AddHostKey(signer)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, rq, _ := nc.Accept()
					go func() {
						for r := range rq {
							if r.Type == "exec" {
								cmd := string(r.Payload[4:])
								r.Reply(true, nil)
								out := "cs-" + name
								if strings.HasPrefix(cmd, "echo ") {
									out = strings.TrimPrefix(cmd, "echo ")
								}
								ch.Write([]byte(out + "\n"))
								ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{0}))
								ch.Close()
							} else if r.WantReply {
								r.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	return l.Addr().String(), signer.PublicKey()
}

func lab1(t *testing.T, mod ...func(*server.Config, *store.Store)) *lab {
	l, err := newLab(0, mod...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.close)
	_, tok, _ := l.St.TokenCreate("family", 0, 0)
	l.token = tok
	return l
}

func onlineNode(t *testing.T, l *lab, name string) (*fakeNode, ssh.PublicKey) {
	addr, hk := fakeSpace(t, name)
	n, rr, err := l.register(name, l.token)
	if err != nil || rr.Code != proto.CodeRegistered {
		t.Fatal(err, rr)
	}
	n.Target = addr
	n.start(l.Srv.HTTPBound)
	if !waitFor(10*time.Second, n.online.Load) {
		t.Fatal("node did not come online")
	}
	return n, hk
}

// jumpRun runs `cmd` on <name> through ssh -J join@broker:<ssh-port>, verifying the space host key.
func jumpRun(brokerSSH, name string, spaceKey ssh.PublicKey, pw, cmd string, tap io.Writer) (string, error) {
	raw, err := net.Dial("tcp", brokerSSH)
	if err != nil {
		return "", err
	}
	var conn net.Conn = raw
	if tap != nil {
		conn = &tapConn{raw, tap}
	}
	jc, jch, jreq, err := ssh.NewClientConn(conn, brokerSSH, &ssh.ClientConfig{User: "join", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		return "", err
	}
	jump := ssh.NewClient(jc, jch, jreq)
	defer jump.Close()
	inner, err := jump.Dial("tcp", name+".cs:22")
	if err != nil {
		return "", err
	}
	hkcb := ssh.FixedHostKey(spaceKey)
	c, ch, rq, err := ssh.NewClientConn(inner, name, &ssh.ClientConfig{User: "user", Auth: []ssh.AuthMethod{ssh.Password(pw)}, HostKeyCallback: hkcb, Timeout: 5 * time.Second})
	if err != nil {
		return "", err
	}
	cl := ssh.NewClient(c, ch, rq)
	defer cl.Close()
	s, err := cl.NewSession()
	if err != nil {
		return "", err
	}
	out, err := s.Output(cmd)
	return strings.TrimSpace(string(out)), err
}

type tapConn struct {
	net.Conn
	w io.Writer
}

func (c *tapConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.w.Write(p[:n])
	return n, err
}
func (c *tapConn) Write(p []byte) (int, error) { c.w.Write(p); return c.Conn.Write(p) }

// S001: registration rules (server 001 R1-R4).
func TestS001Registration(t *testing.T) {
	l := lab1(t)
	if _, rr, _ := l.register("myBrotherPc", l.token); rr.Code != proto.CodeRegistered || rr.NodeID == "" || rr.Status != "active" {
		t.Fatal(rr)
	}
	if _, rr, _ := l.register("MYBROTHERPC", l.token); rr.Code != proto.CodeNameTaken || !strings.Contains(rr.Message, "already taken") {
		t.Fatal("case-insensitive name", rr)
	}
	if _, rr, _ := l.register("other1", "cs1.bad.token"); rr.Code != proto.CodeTokenInvalid {
		t.Fatal(rr)
	}
	if _, rr, _ := l.register("1bad", l.token); rr.Code != proto.CodeBadRequest && rr.Code != proto.CodeNameInvalid {
		t.Fatal(rr)
	}
	// v2: no per-node port anywhere in the API
	resp, _ := http.Get(l.url() + "/v1/nodes")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(b), `"port"`) {
		t.Fatal("port field in node list", string(b))
	}
}

// S002: E2E SSH through the single jump port; the broker relays only ciphertext (server 002).
func TestS002JumpE2EAndNoPlaintext(t *testing.T) {
	l := lab1(t)
	n, hk := onlineNode(t, l, "myBrotherPc")
	tap := &syncBuf{}
	n.tap = tap
	// force the node through the tapped proxy from now on
	l.Srv.Disconnect(n.ID)
	waitFor(5*time.Second, func() bool { return !n.online.Load() })
	if !waitFor(10*time.Second, n.online.Load) {
		t.Fatal("node did not reconnect")
	}
	mark := "secret-marker-7f3a"
	out, err := jumpRun(l.Srv.SSHBound, "myBrotherPc", hk, spacePW, "echo "+mark, tap)
	if err != nil || out != mark {
		t.Fatal(out, err)
	}
	if h, _ := jumpRun(l.Srv.SSHBound, "mybrotherpc", hk, spacePW, "hostname", nil); h != "cs-myBrotherPc" {
		t.Fatal("hostname", h)
	}
	all := tap.Bytes()
	if len(all) < 1000 {
		t.Fatalf("tap too small: %d", len(all))
	}
	for _, s := range []string{mark, spacePW} {
		if bytes.Contains(all, []byte(s)) {
			t.Fatalf("plaintext %q visible on the broker path", s)
		}
	}
	// connection log has metadata
	if !waitFor(3*time.Second, func() bool { ss, _ := l.St.RecentSessions(10); return len(ss) >= 2 && ss[0].EndedAt > 0 }) {
		t.Fatal("session log")
	}
}

// S003: the jump refuses everything except direct-tcpip to <online-node>:22 (server 002 R2-R4).
func TestS003JumpRefusals(t *testing.T) {
	l := lab1(t)
	_, hk := onlineNode(t, l, "myBrotherPc")
	jc, err := ssh.Dial("tcp", l.Srv.SSHBound, &ssh.ClientConfig{User: "join", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	defer jc.Close()
	if s, err := jc.NewSession(); err == nil {
		s.Close()
		t.Fatal("session channel accepted on broker")
	}
	if _, err := jc.Dial("tcp", "myBrotherPc.cs:80"); err == nil {
		t.Fatal("port 80 accepted")
	}
	if _, err := jc.Dial("tcp", "nosuch.cs:22"); err == nil {
		t.Fatal("unknown node accepted")
	}
	if _, err := ssh.Dial("tcp", l.Srv.SSHBound, &ssh.ClientConfig{User: "root", HostKeyCallback: ssh.InsecureIgnoreHostKey()}); err == nil {
		t.Fatal("unknown broker user accepted")
	}
	// wrong space host key (impostor) is rejected by the client before any password is sent
	_, other := fakeSpace(t, "impostor")
	if _, err := jumpRun(l.Srv.SSHBound, "myBrotherPc", other, spacePW, "hostname", nil); err == nil || !strings.Contains(err.Error(), "host key") {
		t.Fatal("impostor key not detected", err)
	}
	_ = hk
}

// S004: pending approval (server 001 R6): pending nodes cannot open a tunnel and are not listed.
func TestS004AdminApproval(t *testing.T) {
	l := lab1(t, func(c *server.Config, st *store.Store) { st.RequireApproval = true })
	addr, _ := fakeSpace(t, "newPc")
	n, rr, _ := l.register("newPc", l.token)
	if rr.Status != "pending" {
		t.Fatal(rr)
	}
	n.Target = addr
	n.start(l.Srv.HTTPBound)
	if waitFor(3*time.Second, n.online.Load) {
		t.Fatal("pending node came online")
	}
	if w, _ := n.lastWelcome.Load().(string); !strings.Contains(w, proto.CodeNodePending) {
		t.Fatal("welcome", w)
	}
	if strings.Contains(nodesJSON(t, l), "newPc") {
		t.Fatal("pending node listed")
	}
	l.St.Approve(n.ID)
	if !waitFor(10*time.Second, n.online.Load) {
		t.Fatal("approved node not online")
	}
}

func nodesJSON(t *testing.T, l *lab) string {
	resp, err := http.Get(l.url() + "/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// S005: reconnect after a network cut, status and load in list (server 004, client 006 R3).
func TestS005ReconnectAndList(t *testing.T) {
	l := lab1(t)
	n, _ := onlineNode(t, l, "myBrotherPc")
	n.cut.Store(true)
	if !waitFor(15*time.Second, func() bool { return strings.Contains(nodesJSON(t, l), `"status":"offline"`) }) {
		t.Fatal("not offline after cut")
	}
	n.cut.Store(false)
	if !waitFor(15*time.Second, func() bool { return strings.Contains(nodesJSON(t, l), `"status":"online"`) }) {
		t.Fatal("no reconnect")
	}
	c, err := ssh.Dial("tcp", l.Srv.SSHBound, &ssh.ClientConfig{User: "list", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, _ := c.NewSession()
	var out []byte
	waitFor(6*time.Second, func() bool {
		s2, _ := c.NewSession()
		out, _ = s2.Output("list")
		return strings.Contains(string(out), "23%")
	})
	s.Close()
	if !strings.Contains(string(out), "myBrotherPc") || !strings.Contains(string(out), "online") || strings.Contains(string(out), "PORT") {
		t.Fatal(string(out))
	}
}

// S006: ban closes the tunnel and blocks re-registration (server 004).
func TestS006Ban(t *testing.T) {
	l := lab1(t)
	n, _ := onlineNode(t, l, "badPc")
	nd, _ := l.St.NodeByName("badPc")
	l.St.BanNode(nd, "test")
	if !waitFor(6*time.Second, func() bool { return !n.online.Load() }) {
		t.Fatal("tunnel not closed")
	}
	if _, rr, _ := l.register("badPc", l.token); rr.Code != proto.CodeNameTaken && rr.Code != proto.CodeNodeBanned {
		t.Fatal(rr)
	}
}

// S007: per-client rate limiting behind a trusted proxy uses X-Forwarded-For (server 002 R6).
func TestS007TrustedProxyRateLimit(t *testing.T) {
	tp, _ := netutil.ParseTrusted("127.0.0.1")
	mk := func(trusted netutil.Trusted) *lab {
		l, err := newLab(3, func(c *server.Config, st *store.Store) { c.TrustedProxies = trusted })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(l.close)
		return l
	}
	get := func(l *lab, xff string) int {
		r, _ := http.NewRequest("GET", l.url()+"/v1/names/someName", nil)
		r.Header.Set("X-Forwarded-For", xff)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	l := mk(tp)
	for i := 0; i < 3; i++ {
		if c := get(l, "198.51.100.1"); c != 200 {
			t.Fatal(i, c)
		}
	}
	if c := get(l, "198.51.100.1"); c != 429 {
		t.Fatal("client A not limited", c)
	}
	if c := get(l, "198.51.100.2"); c != 200 {
		t.Fatal("client B limited by A", c)
	}
	// spoofing: an extra left-most XFF entry must not create a new identity
	if c := get(l, "203.0.113.50, 198.51.100.1"); c != 429 {
		t.Fatal("spoofed XFF bypassed limit", c)
	}
	// untrusted proxy: header ignored, all share 127.0.0.1
	l2 := mk(nil)
	for i := 0; i < 3; i++ {
		get(l2, fmt.Sprintf("198.51.100.%d", i+10))
	}
	if c := get(l2, "198.51.100.99"); c != 429 {
		t.Fatal("XFF trusted without trusted_proxies", c)
	}
}

// S008: admin panel over real HTTP: strong-password login, node list, logout (server 003).
func TestS008PanelLogin(t *testing.T) {
	l := lab1(t, func(c *server.Config, st *store.Store) {
		st.AdminSetPassword("admin", "Panel-Pass-2026!")
	})
	l.Srv.SetPanel(panel.New(panel.Config{}, l.St, l.Srv))
	onlineNode(t, l, "myBrotherPc")
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar}
	resp, err := cl.PostForm(l.url()+"/panel/login", url.Values{"username": {"admin"}, "password": {"wrong-password"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("bad password", resp.StatusCode)
	}
	resp, err = cl.PostForm(l.url()+"/panel/login", url.Values{"username": {"admin"}, "password": {"Panel-Pass-2026!"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "myBrotherPc") || !strings.Contains(string(b), "online") {
		t.Fatal(resp.StatusCode, string(b))
	}
	csrf := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(string(b))[1]
	resp, _ = cl.PostForm(l.url()+"/panel/logout", url.Values{"csrf": {csrf}})
	resp.Body.Close()
	resp, _ = cl.Get(l.url() + "/panel/")
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "Sign in") {
		t.Fatal("not logged out")
	}
	var a []store.AuditEntry
	a, _ = l.St.AuditList(10)
	j, _ := json.Marshal(a)
	if !strings.Contains(string(j), "login.fail") || !strings.Contains(string(j), "login.ok") || strings.Contains(string(j), "Panel-Pass") {
		t.Fatal(string(j))
	}
}

// S009: busy clients behind one IP are slowed, not banned; failed handshakes ban (server 008).
func TestS009FairRateLimit(t *testing.T) {
	l := lab1(t)
	join := func(user string) error {
		c, err := ssh.Dial("tcp", l.Srv.SSHBound, &ssh.ClientConfig{User: user, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
		if err == nil {
			c.Close()
		}
		return err
	}
	for i := 0; i < 20; i++ {
		if err := join("join"); err != nil {
			t.Fatal("burst refused", i, err)
		}
	}
	if join("join") == nil {
		t.Fatal("21st connection within the burst allowed")
	}
	if l.St.IsBanned("ip", "127.0.0.1") {
		t.Fatal("volume alone banned the IP")
	}
	time.Sleep(1100 * time.Millisecond) // 60/min refills one token per second
	if err := join("join"); err != nil {
		t.Fatal("not refilled", err)
	}
	time.Sleep(11 * time.Second) // refill enough tokens for the abuse check
	for i := 0; i < 10; i++ {
		join("root") // unknown user = failed handshake
	}
	if !l.St.IsBanned("ip", "127.0.0.1") {
		t.Fatal("10 failed handshakes did not ban")
	}
}
