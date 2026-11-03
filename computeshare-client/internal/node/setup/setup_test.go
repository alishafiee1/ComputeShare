package setup

import (
	"strings"
	"testing"
)

func TestPasswordAndACL(t *testing.T) {
	if CheckPassword("abc123") == nil || CheckPassword("abc123").Error() != "password too short (min 12)" || CheckPassword("Shiraz-2026-gpu") != nil {
		t.Fatal("password rule")
	}
	eg, in := ACLRules("10.77.0.1", false)
	j := ""
	for _, r := range eg {
		j += strings.Join(r, " ") + "\n"
	}
	for _, w := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10", "destination_port=25", "10.77.0.1/32",
		"0.0.0.0/8", "192.0.0.0/24", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4"} { // 016 R2
		if !strings.Contains(j, w) {
			t.Fatal("missing", w)
		}
	}
	if len(in) != 1 || !strings.Contains(strings.Join(in[0], " "), "destination_port=22") {
		t.Fatal(in)
	}
	q, _ := ACLRules("10.77.0.1", true)
	if !strings.Contains(strings.Join(q[len(q)-1], " "), "::/0") {
		t.Fatal("quota block")
	}
}
