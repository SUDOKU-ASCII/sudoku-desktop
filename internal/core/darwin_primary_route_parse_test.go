package core

import "testing"

func TestParseDarwinScutilNWIOutput(t *testing.T) {
	out := `
Network information
IPv4 network interface: en0
IPv4 primary address: 192.168.1.10
IPv4 router: 192.168.1.1
IPv6 network interface: en0
IPv6 primary address: 2409:8a00::1234
IPv6 router: fe80::1%en0
`
	info := parseDarwinScutilNWIOutput(out)
	if info.Interface4 != "en0" {
		t.Fatalf("unexpected Interface4: %q", info.Interface4)
	}
	if info.Router4 != "192.168.1.1" {
		t.Fatalf("unexpected Router4: %q", info.Router4)
	}
	if info.Interface6 != "en0" {
		t.Fatalf("unexpected Interface6: %q", info.Interface6)
	}
	if info.Router6 != "fe80::1" {
		t.Fatalf("unexpected Router6: %q", info.Router6)
	}
}

func TestParseDarwinScutilNWIOutputFiltersInvalidRouters(t *testing.T) {
	out := `
Network information
IPv4 network interface: en0
IPv4 router: 0.0.0.0
IPv6 network interface: en0
IPv6 router: ::
`
	info := parseDarwinScutilNWIOutput(out)
	if info.Router4 != "" {
		t.Fatalf("expected empty Router4, got %q", info.Router4)
	}
	if info.Router6 != "" {
		t.Fatalf("expected empty Router6, got %q", info.Router6)
	}
}
