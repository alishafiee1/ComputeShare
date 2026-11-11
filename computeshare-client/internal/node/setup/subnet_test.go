package setup

import (
	"net"
	"os"
	"testing"
)

func cidrs(t *testing.T, l ...string) []*net.IPNet {
	var out []*net.IPNet
	for _, s := range l {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func TestPickSubnet(t *testing.T) {
	if g := PickSubnet(nil, "10.77.0.1/24"); g != "10.77.0.1/24" {
		t.Fatal(g)
	}
	// preferred one is taken by a /16 VPN route -> something else, not overlapping
	used := cidrs(t, "10.77.0.0/16", "10.78.0.0/24", "192.168.0.0/24", "10.255.255.254/32")
	g := PickSubnet(used, "10.77.0.1/24")
	_, gn, err := net.ParseCIDR(g)
	if err != nil || g == "10.77.0.1/24" {
		t.Fatal(g, err)
	}
	for _, u := range used {
		if overlaps(gn, u) {
			t.Fatal("overlap", g, u)
		}
	}
	// all of 10/8 and 172.16/12 used -> falls to 192.168.x
	g = PickSubnet(cidrs(t, "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/24"))
	if g != "192.168.100.1/24" {
		t.Fatal(g)
	}
	if PickSubnet(cidrs(t, "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")) != "" {
		t.Fatal("expected none")
	}
}

func TestRouteNets(t *testing.T) {
	r := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0158A8C0\t0003\t0\t0\t35\t00000000\t0\t0\t0\n" +
		"eth1\t0000A8C0\t00000000\t0001\t0\t0\t296\t00FFFFFF\t0\t0\t0\n"
	n := routeNets(r)
	if len(n) != 1 || n[0].String() != "192.168.0.0/24" {
		t.Fatal(n)
	}
}

// Lab only: CSNODE_LIVE_INCUS=1 runs the real init + network steps against the local Incus.
func TestLiveIncusInit(t *testing.T) {
	if os.Getenv("CSNODE_LIVE_INCUS") != "1" {
		t.Skip("lab only")
	}
	for i := 0; i < 2; i++ { // second run proves idempotence
		if err := initIncus(func(s string) { t.Log(s) }); err != nil {
			t.Fatal(err)
		}
		if gw, err := EnsureNetwork(); err != nil {
			t.Fatal(err)
		} else {
			t.Log("csbr0 gateway", gw)
		}
	}
}
