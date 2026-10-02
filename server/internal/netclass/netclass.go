// Package netclass decides whether a request comes from the local network or is remote
// (requirements §3a). Local clients connect directly; remote clients, including all
// Tailscale traffic, get bandwidth-aware quality limits.
package netclass

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
)

type Class string

const (
	Local  Class = "local"
	Remote Class = "remote"
)

// Tailscale address ranges. These are always remote, even though the IPv6 range sits
// inside the private fc00::/7 block.
var tailscalePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

// Proxies we accept X-Forwarded-For from (loopback and Docker bridge gateways).
// `tailscale serve` on the host proxies through these.
var trustedProxyPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("172.16.0.0/12"),
}

type rules struct {
	lan        []netip.Prefix
	remoteHost string // host of the configured remote URL
}

// Classifier is safe for concurrent use; Configure swaps rules atomically.
type Classifier struct{ r atomic.Pointer[rules] }

func New(lanSubnets []string, remoteURL string) *Classifier {
	c := &Classifier{}
	c.Configure(lanSubnets, remoteURL)
	return c
}

// Configure replaces the rules. Invalid subnets are skipped (settings validation rejects them earlier).
func (c *Classifier) Configure(lanSubnets []string, remoteURL string) {
	r := &rules{}
	for _, s := range lanSubnets {
		if p, err := netip.ParsePrefix(s); err == nil {
			r.lan = append(r.lan, p.Masked())
		}
	}
	if u, err := url.Parse(remoteURL); err == nil {
		r.remoteHost = strings.ToLower(u.Hostname())
	}
	c.r.Store(r)
}

// Classify returns the network class of the request's client.
func (c *Classifier) Classify(req *http.Request) Class {
	r := c.r.Load()

	// Requests addressed to the Tailscale name are remote, whatever proxy they came through.
	host := strings.ToLower(hostOnly(req.Host))
	if strings.HasSuffix(host, ".ts.net") || (r.remoteHost != "" && host == r.remoteHost) {
		return Remote
	}

	ip := ClientIP(req)
	if !ip.IsValid() {
		return Remote
	}
	for _, p := range tailscalePrefixes {
		if p.Contains(ip) {
			return Remote
		}
	}
	if ip.IsLoopback() {
		return Local
	}
	for _, p := range r.lan {
		if p.Contains(ip) {
			return Local
		}
	}
	return Remote
}

// ClientIP returns the originating client address, honouring X-Forwarded-For only when the
// direct peer is a trusted local proxy.
func ClientIP(req *http.Request) netip.Addr {
	peer := parseAddr(req.RemoteAddr)
	if !peer.IsValid() || !trusted(peer) {
		return peer
	}
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		// Right-most untrusted entry is the client as seen by our trusted proxy chain.
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			a := parseAddr(strings.TrimSpace(parts[i]))
			if a.IsValid() && !trusted(a) {
				return a
			}
		}
	}
	return peer
}

func trusted(a netip.Addr) bool {
	for _, p := range trustedProxyPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func parseAddr(s string) netip.Addr {
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}
