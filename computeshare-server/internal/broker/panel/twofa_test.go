package panel

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alishafiee1/computeshare-server/internal/broker/store"
)

func with2FA(t *testing.T) (*Panel, *store.Store, []byte, []string) {
	p, st, _ := setup(t)
	secret, err := st.Admin2FAStart("admin")
	if err != nil {
		t.Fatal(err)
	}
	codes, err := st.Admin2FAConfirm("admin", store.TOTPCodeAt(secret, st.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return p, st, secret, codes
}

// nextCode is the code for the next time step: enabling 2FA used the current step, and a step works only once.
func nextCode(secret []byte, st *store.Store) string {
	return store.TOTPCodeAt(secret, st.Now().Add(30*time.Second))
}

func passwordStep(t *testing.T, p *Panel, ip string) (token string) {
	w := do(p, "POST", "/panel/login", url.Values{"username": {"admin"}, "password": {goodPW}}, nil, ip)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "authenticator") {
		t.Fatalf("expected the second step page, got %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "cs_panel" {
			t.Fatal("a session cookie was set before the second step")
		}
	}
	m := regexp.MustCompile(`name="token" value="([0-9a-f]+)"`).FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("no pending token in the form")
	}
	return m[1]
}

// server 007 R4: with 2FA on, a correct password alone gives no session; the code finishes the login once.
func TestLoginNeedsSecondStep(t *testing.T) {
	p, st, secret, _ := with2FA(t)
	tok := passwordStep(t, p, "10.0.0.1")
	w := do(p, "POST", "/panel/login/2fa", url.Values{"token": {tok}, "code": {"000000"}}, nil, "10.0.0.1")
	if w.Code != 401 || !strings.Contains(w.Body.String(), "not valid") {
		t.Fatalf("wrong code: %d", w.Code)
	}
	w = do(p, "POST", "/panel/login/2fa", url.Values{"token": {tok}, "code": {nextCode(secret, st)}}, nil, "10.0.0.1")
	if w.Code != 303 {
		t.Fatalf("right code: %d %s", w.Code, w.Body.String())
	}
	var got bool
	for _, c := range w.Result().Cookies() {
		got = got || c.Name == "cs_panel"
	}
	if !got {
		t.Fatal("no cookie after the second step")
	}
	// the token is single use
	w = do(p, "POST", "/panel/login/2fa", url.Values{"token": {tok}, "code": {"123456"}}, nil, "10.0.0.1")
	if w.Code != 401 || !strings.Contains(w.Body.String(), "expired") {
		t.Fatalf("token reused: %d", w.Code)
	}
}

func TestSecondStepBackupCodeAndAudit(t *testing.T) {
	p, st, _, codes := with2FA(t)
	tok := passwordStep(t, p, "10.0.0.2")
	w := do(p, "POST", "/panel/login/2fa", url.Values{"token": {tok}, "code": {codes[3]}}, nil, "10.0.0.2")
	if w.Code != 303 {
		t.Fatalf("backup code: %d", w.Code)
	}
	var actions []string
	au, _ := st.AuditList(20)
	for _, a := range au {
		actions = append(actions, a.Action)
	}
	joined := strings.Join(actions, ",")
	for _, want := range []string{"login.2fa.required", "login.ok"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("audit lacks %s: %s", want, joined)
		}
	}
	if strings.Contains(joined, codes[3]) {
		t.Fatal("a code leaked into the audit log")
	}
}

// server 007 R5: wrong codes count toward the login limit, and five wrong codes burn the token.
func TestSecondStepRateLimit(t *testing.T) {
	p, st, secret, _ := with2FA(t)
	tok := passwordStep(t, p, "10.0.0.3")
	for i := 0; i < pendingTries; i++ {
		w := do(p, "POST", "/panel/login/2fa", url.Values{"token": {tok}, "code": {"999999"}}, nil, "10.0.0.3")
		if w.Code != 401 {
			t.Fatalf("try %d: %d", i, w.Code)
		}
	}
	w := do(p, "POST", "/panel/login/2fa", url.Values{"token": {tok}, "code": {nextCode(secret, st)}}, nil, "10.0.0.3")
	if w.Code != 401 || !strings.Contains(w.Body.String(), "expired") {
		t.Fatalf("the token should be gone after %d wrong codes: %d", pendingTries, w.Code)
	}
	if !p.Locked("10.0.0.3", "admin") {
		t.Fatal("the failures did not count toward the login limit")
	}
}

func TestSecondStepTokenBoundToIPAndTime(t *testing.T) {
	p, st, secret, _ := with2FA(t)
	tok := passwordStep(t, p, "10.0.0.4")
	good := url.Values{"token": {tok}, "code": {nextCode(secret, st)}}
	if w := do(p, "POST", "/panel/login/2fa", good, nil, "10.9.9.9"); w.Code != 401 {
		t.Fatalf("token used from another IP: %d", w.Code)
	}
	tok = passwordStep(t, p, "10.0.0.4")
	now := time.Now()
	p.Now = func() time.Time { return now.Add(pendingTTL + time.Second) }
	good.Set("token", tok)
	if w := do(p, "POST", "/panel/login/2fa", good, nil, "10.0.0.4"); w.Code != 401 {
		t.Fatalf("expired token accepted: %d", w.Code)
	}
}

// without 2FA nothing changes for the existing flow.
func TestNo2FAKeepsOneStepLogin(t *testing.T) {
	p, _, _ := setup(t)
	w := do(p, "POST", "/panel/login", url.Values{"username": {"admin"}, "password": {goodPW}}, nil, "10.0.0.5")
	if w.Code != http.StatusSeeOther {
		t.Fatal(w.Code)
	}
}
