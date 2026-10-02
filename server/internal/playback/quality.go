package playback

import "marquee/internal/settings"

// QualityInputs feed the automatic quality choice (requirements §3a).
type QualityInputs struct {
	Remote bool
	// RequestedKbps is the app's quality setting for this session; 0 = original/automatic.
	RequestedKbps int
	// UserPrefKbps is the user's own default for this network (Account → Playback); 0 = none.
	UserPrefKbps int
	// UserCapKbps is the admin-set remote limit for this user; 0 = none.
	UserCapKbps int
	// MeasuredKbps is the client's measured download speed; 0 = unknown.
	MeasuredKbps int
	// OtherRemoteKbps is what other active remote sessions are using.
	OtherRemoteKbps  int
	OtherRemoteCount int
}

// Headroom keeps the stream below the measured connection speed so it doesn't stall.
const Headroom = 0.7

// MaxKbps returns the session's bitrate ceiling (0 = no limit) and a short explanation.
func MaxKbps(in QualityInputs, ra settings.RemoteAccess) (int, string) {
	limit, why := 0, ""
	apply := func(v int, reason string) {
		if v > 0 && (limit == 0 || v < limit) {
			limit, why = v, reason
		}
	}
	apply(in.RequestedKbps, "quality setting")
	apply(in.UserPrefKbps, "your quality preference")
	if !in.Remote {
		return limit, why
	}
	apply(in.UserCapKbps, "your remote limit")
	if in.MeasuredKbps > 0 {
		apply(int(float64(in.MeasuredKbps)*Headroom), "connection speed")
	}
	apply(ra.PerStreamRemoteLimitKbps, "server per-stream limit")
	if ra.TotalRemoteLimitKbps > 0 {
		apply(max(ra.TotalRemoteLimitKbps-in.OtherRemoteKbps, ra.MinStreamKbps), "server remote limit")
	}
	if ra.UploadSpeedKbps > 0 {
		// Fair share of the upload among this and the other remote streams.
		share := int(float64(ra.UploadSpeedKbps)*0.9) / (in.OtherRemoteCount + 1)
		apply(max(share, ra.MinStreamKbps), "shared upload bandwidth")
	}
	return limit, why
}
