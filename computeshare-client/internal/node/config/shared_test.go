package config

import "testing"

func TestParseGB(t *testing.T) {
	for in, want := range map[string]float64{"4GiB": 4, "20GB": 20, "20gb": 20, "512MiB": 0.5, "1.5T": 1536, "8": 0} {
		got, ok := ParseGB(in)
		if in == "8" { // a bare number has no unit: treated as GB
			if !ok || got != 8 {
				t.Fatalf("%s: %v %v", in, got, ok)
			}
			continue
		}
		if !ok || got != want {
			t.Fatalf("%s: got %v ok=%v want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "off", "unlimited", "abc", "0GB", "-1GB"} {
		if _, ok := ParseGB(in); ok {
			t.Fatalf("%q should mean no limit", in)
		}
	}
}

// umbrella card 016 R3/R4: only limited fields are set; unlimited ones stay empty.
func TestSharedSpecs(t *testing.T) {
	s := Limits{CPU: "2", Memory: "4GiB", Disk: "20GiB", GPU: true}.SharedSpecs("GTX 1660 Ti")
	if s.CPU != "2c" || s.RAMGB != 4 || s.DiskGB != 20 || s.GPU != "GTX 1660 Ti" {
		t.Fatalf("%+v", s)
	}
	u := Limits{}.SharedSpecs("GTX 1660 Ti")
	if u.CPU != "" || u.RAMGB != 0 || u.DiskGB != 0 || u.GPU != "" {
		t.Fatalf("unlimited must stay empty: %+v", u)
	}
}
