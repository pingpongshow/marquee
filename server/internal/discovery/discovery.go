// Package discovery advertises the server on the local network with Bonjour/mDNS
// (_marquee._tcp), so apps on the LAN find it without typing an address. Only interfaces
// with an address in the configured LAN subnets are used (not Docker or Tailscale).
package discovery

import (
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/grandcat/zeroconf"
)

const Service = "_marquee._tcp"

type Advertiser struct {
	mu     sync.Mutex
	server *zeroconf.Server
}

// Info is what's announced in the TXT record.
type Info struct {
	ServerID, Name, Version string
	Port                    int
}

// lanInterfaces returns interfaces with an IPv4 address inside one of subnets.
func lanInterfaces(subnets []string) ([]net.Interface, []string) {
	var prefixes []netip.Prefix
	for _, s := range subnets {
		if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil {
			prefixes = append(prefixes, p)
		}
	}
	ifaces, _ := net.Interfaces()
	var out []net.Interface
	var ips []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 || ifc.Flags&net.FlagLoopback != 0 || virtual(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP)
			if !ok {
				continue
			}
			ip = ip.Unmap()
			for _, p := range prefixes {
				if p.Contains(ip) {
					out = append(out, ifc)
					ips = append(ips, ip.String())
					goto next
				}
			}
		}
	next:
	}
	return out, ips
}

// Apply starts, restarts or stops advertising.
func (a *Advertiser) Apply(enabled bool, info Info, subnets []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server != nil {
		a.server.Shutdown()
		a.server = nil
	}
	if !enabled {
		return
	}
	ifaces, ips := lanInterfaces(subnets)
	if len(ifaces) == 0 {
		slog.Info("bonjour: no network interface on the LAN subnets; not advertising (in Docker this needs host networking)")
		return
	}
	txt := []string{"id=" + info.ServerID, "name=" + info.Name, "version=" + info.Version}
	host := "marquee"
	if len(info.ServerID) >= 8 {
		host += "-" + info.ServerID[:8]
	}
	s, err := zeroconf.RegisterProxy(info.Name, Service, "local.", info.Port, host, ips, txt, ifaces)
	if err != nil {
		slog.Warn("bonjour", "err", err)
		return
	}
	a.server = s
	names := make([]string, len(ifaces))
	for i, f := range ifaces {
		names[i] = f.Name
	}
	slog.Info("bonjour: advertising", "service", Service, "interfaces", strings.Join(names, ","))
}

func (a *Advertiser) Stop() { a.Apply(false, Info{}, nil) }

// virtual reports container, VM and VPN interfaces, which share private address ranges
// with the LAN but aren't where apps live.
func virtual(name string) bool {
	if name == "docker0" {
		return true
	}
	for _, p := range []string{"br-", "veth", "virbr", "vnet", "tailscale", "tun", "tap", "wg", "utun", "cni", "flannel", "cali", "zt"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
