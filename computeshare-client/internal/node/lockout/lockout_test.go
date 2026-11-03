package lockout

import "testing"

func TestLockout(t *testing.T) {
	l := New()
	l.Map(5555, "9.9.9.9")
	for i := 0; i < 5; i++ {
		l.Line("Failed password for user from 10.77.0.1 port 5555 ssh2")
	}
	if !l.Blocked("9.9.9.9") || l.Blocked("1.1.1.1") {
		t.Fatal("lockout")
	}
}

// client 016 R4: an agent offering a wrong key before the right one is not a failure;
// a key that failed and then gave up is one failure, as is "too many attempts".
func TestKeyThenSuccess(t *testing.T) {
	l := New()
	l.Map(6000, "8.8.8.8")
	for i := 0; i < 10; i++ {
		l.Line("Failed publickey for bob from 10.77.0.1 port 6000 ssh2: ED25519 SHA256:x")
		l.Line("Failed publickey for bob from 10.77.0.1 port 6000 ssh2: RSA SHA256:y")
		l.Line("Accepted publickey for bob from 10.77.0.1 port 6000 ssh2: ED25519 SHA256:z")
		l.Line("Disconnected from user bob 10.77.0.1 port 6000")
	}
	if l.Blocked("8.8.8.8") {
		t.Fatal("successful key logins counted as failures")
	}
	for i := 0; i < 4; i++ {
		l.Line("Failed publickey for bob from 10.77.0.1 port 6000 ssh2: ED25519 SHA256:x")
		if l.Line("Connection closed by authenticating user bob 10.77.0.1 port 6000 [preauth]") != "8.8.8.8" {
			t.Fatal("failed key then close not counted")
		}
	}
	if l.Line("error: maximum authentication attempts exceeded for bob from 10.77.0.1 port 6000 ssh2 [preauth]") != "8.8.8.8" || !l.Blocked("8.8.8.8") {
		t.Fatal("5th failure must block")
	}
	l.Map(6001, "7.7.7.7")
	if l.Line("Connection closed by 10.77.0.1 port 6001 [preauth]") != "" {
		t.Fatal("a plain close without a failed key is not a failure")
	}
}
