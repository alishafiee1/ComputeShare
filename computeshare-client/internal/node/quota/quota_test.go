package quota

import (
	"testing"
	"time"
)

func TestDayRollover(t *testing.T) {
	var c Counter
	d := time.Date(2026, 10, 16, 20, 0, 0, 0, Zone)
	c.Update(d, 100, 1000)
	if c.Update(d, 1200, 1000) != true {
		t.Fatal("quota should be reached")
	}
	if c.Update(d.Add(5*time.Hour), 1300, 1000) {
		t.Fatal("new Tehran day must reset")
	}
	if c.Update(d.Add(5*time.Hour), 5000, 0) {
		t.Fatal("0 = unlimited")
	}
}
