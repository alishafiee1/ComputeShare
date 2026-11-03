package list

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alishafiee1/computeshare-client/proto"
)

func f(v float64) *float64 { return &v }

func TestText(t *testing.T) {
	if s := Text(nil, 0); !strings.Contains(s, "NAME") || !strings.Contains(s, "no nodes") {
		t.Fatal(s)
	}
	rows := []proto.NodeRow{
		{Name: "myBrotherPc", Status: "online", Specs: proto.Specs{CPU: "8c", RAMGB: 16, GPU: "RTX 4070"}, Load: proto.Load{Now: f(23), Avg24h: f(41)}, Limits: proto.Limits{Bandwidth: "20Mbit"}, UptimeS: 3700},
		{Name: "officeBox", Status: "offline", LastSeen: 1000},
	}
	s := Text(rows, 1000+720)
	for _, w := range []string{"NAME", "STATUS", "LOAD now/avg24h", "NET-LIMIT", "myBrotherPc", "online", "23%/41%", "officeBox", "seen 12m", "RTX 4070"} {
		if !strings.Contains(s, w) {
			t.Fatalf("missing %q in\n%s", w, s)
		}
	}
	if !strings.Contains(strings.Split(s, "\n")[2], " - ") && !strings.Contains(s, "-/-") {
		t.Fatal("no gpu -> -")
	}
}

// umbrella card 016 R3/R4: shared values are shown; an unlimited field shows the host value with a star.
func TestTextSharedAndUnlimited(t *testing.T) {
	r := proto.NodeRow{Name: "box", Status: "online", Specs: proto.Specs{CPU: "2c", RAMGB: 12, DiskGB: 1024}, Unlimited: []string{"ram", "disk"}}
	s := Text([]proto.NodeRow{r}, 0)
	line := strings.Split(s, "\n")[1]
	if !strings.Contains(line, "2c ") || strings.Contains(line, "2c*") || !strings.Contains(line, "12G*") {
		t.Fatalf("columns wrong:\n%s", s)
	}
}

func TestJSONStruct(t *testing.T) {
	s := JSON([]proto.NodeRow{{Name: "myBrotherPc", Status: "online"}})
	var rows []proto.NodeRow
	if err := json.Unmarshal([]byte(s), &rows); err != nil || rows[0].Name != "myBrotherPc" {
		t.Fatal(err)
	}
	for _, bad := range []string{"password", "authorized_keys", "token"} {
		if strings.Contains(s, bad) {
			t.Fatal("credential field in output:", bad)
		}
	}
	if JSON(nil) != "[]\n" {
		t.Fatal("empty")
	}
}
