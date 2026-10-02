package playback

import (
	"testing"

	"marquee/internal/settings"
)

func TestMaxKbps(t *testing.T) {
	ra := settings.RemoteAccess{UploadSpeedKbps: 40000, MinStreamKbps: 1500}
	if k, _ := MaxKbps(QualityInputs{}, ra); k != 0 {
		t.Errorf("local with no preferences should be unlimited, got %d", k)
	}
	if k, why := MaxKbps(QualityInputs{Remote: true, MeasuredKbps: 10000}, ra); k != 7000 || why != "connection speed" {
		t.Errorf("remote measured: %d %s", k, why)
	}
	if k, why := MaxKbps(QualityInputs{Remote: true, OtherRemoteCount: 5}, ra); k != 6000 || why != "shared upload bandwidth" {
		t.Errorf("6 remote streams share 36 Mbps: %d %s", k, why)
	}
	if k, _ := MaxKbps(QualityInputs{Remote: true, OtherRemoteCount: 50}, ra); k != 1500 {
		t.Errorf("floor: %d", k)
	}
	if k, why := MaxKbps(QualityInputs{Remote: true, UserCapKbps: 2000, RequestedKbps: 8000}, ra); k != 2000 || why != "your remote limit" {
		t.Errorf("user cap: %d %s", k, why)
	}
	if k, _ := MaxKbps(QualityInputs{UserCapKbps: 2000}, ra); k != 0 {
		t.Errorf("remote caps must not apply at home: %d", k)
	}
}
