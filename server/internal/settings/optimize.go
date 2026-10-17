package settings

import (
	"strings"
)

// Optimize configures the library optimizer (Library Health → Large files): re-encoding
// oversized movie and episode files to save space (admins only).
type Optimize struct {
	// Presets are the admin's own presets (the built-in ones are always there too).
	Presets []OptimizePreset `json:"presets"`
	// DefaultPreset is the preset picked first (a built-in or custom id).
	DefaultPreset string `json:"defaultPreset"`
	// Concurrency is how many files are re-encoded at once (1–4).
	Concurrency int `json:"concurrency"`
	// MinSavingsPercent: a result must be at least this much smaller than the original to
	// replace it (5–90); otherwise it's discarded and the file marked skipped.
	MinSavingsPercent int `json:"minSavingsPercent"`
	// PauseWhileWatching pauses re-encodes while anyone is watching anything, resuming
	// about 15 seconds after.
	PauseWhileWatching bool `json:"pauseWhileWatching"`
	// WindowOnly runs re-encodes only inside the maintenance window.
	WindowOnly bool `json:"windowOnly"`
	// Paused holds the whole queue (an admin's pause).
	Paused bool `json:"paused"`
	// KeepOriginals moves originals to the media trash (restorable until it's emptied).
	// Off (the default), an original is deleted once its new file is verified and swapped
	// in, freeing the space at once.
	KeepOriginals bool `json:"keepOriginals"`
}

// OptimizePreset is how files are re-encoded.
type OptimizePreset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// MaxHeight caps the resolution (never upscaled): 2160, 1080, 720 or 480; 0 keeps it.
	MaxHeight int `json:"maxHeight"`
	// Codec is hevc, h264 or av1 (AV1 only when the server has an AV1 encoder).
	Codec string `json:"codec"`
	// Quality is a constant-quality level (high, balanced, small), used unless VideoKbps
	// (a target bitrate) is set.
	Quality   string `json:"quality"`
	VideoKbps int    `json:"videoKbps"`
	// Audio: copy (every track as it is), aac_stereo or aac_51 (every track converted, at
	// most 5.1). Every audio track (all languages, commentary) and every subtitle is always
	// kept.
	Audio string `json:"audio"`
	// HDR: keep (HDR10, 10-bit HEVC or AV1) or tonemap (to SDR).
	HDR string `json:"hdr"`
	// BuiltIn marks Marquee's own presets (not stored).
	BuiltIn bool `json:"builtIn,omitempty"`
}

// BuiltInOptimizePresets are the presets every server has.
func BuiltInOptimizePresets() []OptimizePreset {
	return []OptimizePreset{
		{ID: "1080p-hevc-balanced", Name: "1080p HEVC (balanced)", MaxHeight: 1080, Codec: "hevc", Quality: "balanced",
			Audio: "copy", HDR: "keep", BuiltIn: true},
		{ID: "1080p-hevc-small", Name: "1080p HEVC (small)", MaxHeight: 1080, Codec: "hevc", Quality: "small",
			Audio: "aac_51", HDR: "keep", BuiltIn: true},
		{ID: "720p-hevc", Name: "720p HEVC", MaxHeight: 720, Codec: "hevc", Quality: "balanced",
			Audio: "aac_51", HDR: "tonemap", BuiltIn: true},
		{ID: "same-hevc", Name: "Same resolution, HEVC", MaxHeight: 0, Codec: "hevc", Quality: "balanced",
			Audio: "copy", HDR: "keep", BuiltIn: true},
	}
}

// AllPresets are the built-in presets followed by the admin's own.
func (o Optimize) AllPresets() []OptimizePreset {
	out := BuiltInOptimizePresets()
	for _, p := range o.Presets {
		p.BuiltIn = false
		out = append(out, p)
	}
	return out
}

// Preset finds a preset by id.
func (o Optimize) Preset(id string) (OptimizePreset, bool) {
	for _, p := range o.AllPresets() {
		if p.ID == id {
			return p, true
		}
	}
	return OptimizePreset{}, false
}

func defaultOptimize() Optimize {
	return Optimize{DefaultPreset: "1080p-hevc-balanced", Concurrency: 1, MinSavingsPercent: 20, PauseWhileWatching: true}
}

func (o Optimize) validate() error {
	if o.Concurrency < 1 || o.Concurrency > 4 {
		return invalid("files optimized at once must be 1–4")
	}
	if p := o.MinSavingsPercent; p < 5 || p > 90 {
		return invalid("the smallest saving worth keeping must be 5–90%%")
	}
	if len(o.Presets) > 30 {
		return invalid("at most 30 custom presets")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, b := range BuiltInOptimizePresets() {
		ids[b.ID], names[strings.ToLower(b.Name)] = true, true
	}
	for _, p := range o.Presets {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		switch {
		case name == "":
			return invalid("each preset needs a name")
		case names[name]:
			return invalid("there are two presets named %q", strings.TrimSpace(p.Name))
		case p.ID == "" || ids[p.ID]:
			return invalid("each preset needs its own id")
		}
		ids[p.ID], names[name] = true, true
		if err := p.Validate(); err != nil {
			return err
		}
	}
	if o.DefaultPreset != "" && !ids[o.DefaultPreset] {
		return invalid("the default preset doesn't exist")
	}
	return nil
}

// Validate checks a preset's options.
func (p OptimizePreset) Validate() error {
	switch p.MaxHeight {
	case 0, 2160, 1080, 720, 480:
	default:
		return invalid("the preset %q: the resolution must be 2160, 1080, 720, 480 or 0 (keep)", p.Name)
	}
	switch p.Codec {
	case "hevc", "h264", "av1":
	default:
		return invalid("the preset %q: the codec must be hevc, h264 or av1", p.Name)
	}
	switch p.Quality {
	case "high", "balanced", "small":
	default:
		return invalid("the preset %q: the quality must be high, balanced or small", p.Name)
	}
	if p.VideoKbps < 0 || p.VideoKbps > 100000 {
		return invalid("the preset %q: the video bitrate must be 0 (use the quality) to 100000 kbps", p.Name)
	}
	switch p.Audio {
	case "copy", "aac_stereo", "aac_51":
	default:
		return invalid("the preset %q: the audio must be copy, aac_stereo or aac_51", p.Name)
	}
	switch p.HDR {
	case "keep", "tonemap":
	default:
		return invalid("the preset %q: HDR must be keep or tonemap", p.Name)
	}
	return nil
}
