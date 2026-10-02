package discovery

import "testing"

func TestVirtualInterfaces(t *testing.T) {
	for name, want := range map[string]bool{"br0": false, "eth0": false, "en0": false, "bond0": false,
		"docker0": true, "br-b5539d988f41": true, "veth12ab": true, "virbr0": true, "tailscale0": true, "wg0": true} {
		if got := virtual(name); got != want {
			t.Errorf("virtual(%q) = %v", name, got)
		}
	}
}
