package approval

import "testing"

// client 015 R9: gateway logins get their answer through Decided; old in-space entries do not.
func TestGatewayDecisions(t *testing.T) {
	t.Setenv("CSNODE_DIR", t.TempDir())
	k := AddGateway("key", "ali", "SHA256:abc", "1.1.1.1")
	if AddGateway("key", "ali", "SHA256:abc", "1.1.1.1") != k {
		t.Fatal("duplicate pending entry")
	}
	if TakeDecision(k) != "" {
		t.Fatal("decided before the owner answered")
	}
	if _, err := Approve(k, false); err != nil {
		t.Fatal(err)
	}
	if TakeDecision(k) != "key" || TakeDecision(k) != "" {
		t.Fatal("key decision")
	}
	if len(Load().Keys) != 0 {
		t.Fatal("gateway key leaked into the node-wide list")
	}
	s := AddGateway("session", "ali", "", "2.2.2.2")
	Deny(s)
	if TakeDecision(s) != "deny" {
		t.Fatal("deny")
	}
	s2 := AddGateway("session", "sara", "", "3.3.3.3")
	Approve(s2, true)
	if TakeDecision(s2) != "once" || len(Load().IPs) != 0 {
		t.Fatal("once")
	}
	AddPendingKey("SHA256:old")
	d := Load()
	Approve(d.Pending[0].ID, false)
	after := Load()
	if len(after.Decided) != 0 || !after.KeyApproved("SHA256:old") {
		t.Fatal("v2 approval path changed")
	}
}
