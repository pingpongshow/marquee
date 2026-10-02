package netclass

import (
	"net/http/httptest"
	"testing"
)

func TestClassify(t *testing.T) {
	c := New([]string{"10.1.1.0/24"}, "https://tower.tail1234.ts.net")
	tests := []struct {
		name, remoteAddr, host, xff string
		want                        Class
	}{
		{"lan client", "10.1.1.50:5000", "10.1.1.10:32500", "", Local},
		{"other private subnet not configured", "192.168.5.2:5000", "10.1.1.10:32500", "", Remote},
		{"tailscale ipv4 direct", "100.101.102.103:5000", "100.64.0.1:32500", "", Remote},
		{"tailscale ipv6 direct", "[fd7a:115c:a1e0::1]:5000", "x", "", Remote},
		{"tailscale serve via loopback", "127.0.0.1:5000", "tower.tail1234.ts.net", "", Remote},
		{"any ts.net host", "172.17.0.1:5000", "other.tailabcd.ts.net", "", Remote},
		{"loopback", "127.0.0.1:5000", "localhost:32500", "", Local},
		{"xff from trusted proxy", "127.0.0.1:5000", "media.example.com", "203.0.113.9", Remote},
		{"xff lan via proxy", "127.0.0.1:5000", "media.local", "10.1.1.77", Local},
		{"xff ignored from another container", "172.17.0.3:5000", "media.local", "10.1.1.77", Remote}, // a spoofed home address is not taken
		{"xff ignored from untrusted peer", "203.0.113.5:5000", "x", "10.1.1.77", Remote},
		{"public internet", "203.0.113.5:5000", "x", "", Remote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			r.Host = tt.host
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := c.Classify(r); got != tt.want {
				t.Errorf("Classify() = %s, want %s", got, tt.want)
			}
		})
	}
}
