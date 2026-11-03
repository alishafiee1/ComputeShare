package proto

import "testing"

// umbrella card 016 R3/R4: shared amounts replace the host totals; unlimited fields keep the host value and are named.
func TestSharedView(t *testing.T) {
	host := Specs{CPU: "12c", RAMGB: 12, DiskGB: 1024, GPU: "RTX"}
	show, h, unl := SharedView(host, &Specs{CPU: "2c", RAMGB: 4, DiskGB: 20})
	if show.CPU != "2c" || show.RAMGB != 4 || show.DiskGB != 20 || show.GPU != "" || len(unl) != 0 {
		t.Fatalf("%+v %v", show, unl)
	}
	if h == nil || h.CPU != "12c" || h.DiskGB != 1024 {
		t.Fatalf("host %+v", h)
	}
	show, _, unl = SharedView(host, &Specs{CPU: "2c", GPU: "RTX"})
	if show.RAMGB != 12 || show.DiskGB != 1024 || show.GPU != "RTX" || len(unl) != 2 || unl[0] != "ram" || unl[1] != "disk" {
		t.Fatalf("%+v %v", show, unl)
	}
	show, h, unl = SharedView(host, nil)
	if show != host || h != nil || unl != nil {
		t.Fatalf("nil shared must return the host unchanged: %+v %v %v", show, h, unl)
	}
}
