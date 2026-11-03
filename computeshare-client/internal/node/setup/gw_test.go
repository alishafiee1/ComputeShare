package setup

import "testing"

func TestGateways(t *testing.T) {
	r := "Iface\tDestination\tGateway\teth0\nunused\neth0\t00000000\t0120A8C0\t0003\neth0\t0020A8C0\t00000000\n"
	g := gateways(r)
	if len(g) != 1 || g[0] != "192.168.32.1/32" {
		t.Fatal(g)
	}
}
