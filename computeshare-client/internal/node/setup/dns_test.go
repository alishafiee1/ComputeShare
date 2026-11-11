package setup

import (
	"reflect"
	"testing"
)

func TestPickDNSUpstreams(t *testing.T) {
	gw := "10.77.0.1"
	cases := []struct {
		name, override string
		confs          []string
		want           []string
		hostOK         bool
	}{
		{"wsl tunnel", "", []string{"nameserver 10.255.255.254\n"}, FallbackDNS, false},
		{"loopback stub", "", []string{"nameserver 127.0.0.53\n"}, FallbackDNS, false},
		{"empty", "", []string{"", ""}, FallbackDNS, false},
		{"real lan resolver", "", []string{"nameserver 192.168.1.1\nnameserver 8.8.4.4\n"}, []string{"192.168.1.1", "8.8.4.4"}, true},
		{"resolved upstream beats stub", "", []string{"nameserver 9.9.9.9\n", "nameserver 127.0.0.53\n"}, []string{"9.9.9.9"}, true},
		{"stub first file, real second", "", []string{"nameserver 127.0.0.53\n", "nameserver 1.0.0.1\n"}, []string{"1.0.0.1"}, true},
		{"mixed keeps usable", "", []string{"nameserver 127.0.0.1\nnameserver 10.255.255.254\nnameserver 4.2.2.2\n"}, []string{"4.2.2.2"}, true},
		{"gateway itself rejected", "", []string{"nameserver 10.77.0.1\n"}, FallbackDNS, false},
		{"ipv6 and zone skipped", "", []string{"nameserver fe80::1%eth0\nnameserver 2001:db8::1\n"}, FallbackDNS, false},
		{"override wins", "9.9.9.9, 1.1.1.1", []string{"nameserver 192.168.1.1\n"}, []string{"9.9.9.9", "1.1.1.1"}, false},
		{"override garbage falls back to host", "foo,::1", []string{"nameserver 192.168.1.1\n"}, []string{"192.168.1.1"}, true},
	}
	for _, c := range cases {
		got, ok := PickDNSUpstreams(c.override, gw, c.confs...)
		if !reflect.DeepEqual(got, c.want) || ok != c.hostOK {
			t.Errorf("%s: got %v,%v want %v,%v", c.name, got, ok, c.want, c.hostOK)
		}
	}
}

func TestDNSRaw(t *testing.T) {
	want := "# csnode-managed\nserver=1.1.1.1\nserver=8.8.8.8"
	if got := DNSRaw([]string{"1.1.1.1", "8.8.8.8"}); got != want {
		t.Fatalf("got %q", got)
	}
}
