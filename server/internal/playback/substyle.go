package playback

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SubtitleStyle is how a person wants text subtitles to look (PLAY-20). Empty fields use
// the defaults: medium, #FFFFFF, outline, bottom.
type SubtitleStyle struct {
	Size, Color, Background, Position string
}

// Font sizes in libass units: FFmpeg turns SRT/WebVTT into ASS with PlayResY 288 and a
// 16-point default, so medium keeps the look burned-in subtitles always had (about 5.5% of
// the picture height) and the others step around it.
var subtitleSizes = map[string]int{"small": 13, "medium": 16, "large": 20, "huge": 25}

const (
	subtitleMarginBottom = 10 // FFmpeg's default MarginV
	subtitleMarginRaised = 50 // about a sixth of the picture up: clear of controls and lower thirds
)

// assColor turns #RRGGBB into ASS's &HAABBGGRR (alpha 00 = opaque).
func assColor(hex string, alpha byte) string {
	if len(hex) != 7 || hex[0] != '#' {
		hex = "#FFFFFF"
	}
	r, g, b := hex[1:3], hex[3:5], hex[5:7]
	return fmt.Sprintf("&H%02X%s%s%s", alpha, strings.ToUpper(b), strings.ToUpper(g), strings.ToUpper(r))
}

// ForceStyle is the libass style override for burning text subtitles in this style, for the
// subtitles filter's force_style option; "" when nothing is set (FFmpeg's own defaults).
func (st SubtitleStyle) ForceStyle() string {
	if st == (SubtitleStyle{}) {
		return ""
	}
	size, ok := subtitleSizes[st.Size]
	if !ok {
		size = subtitleSizes["medium"]
	}
	parts := []string{fmt.Sprintf("FontSize=%d", size), "PrimaryColour=" + assColor(st.Color, 0)}
	switch st.Background {
	case "none": // a soft drop shadow only
		parts = append(parts, "BorderStyle=1", "Outline=0", "Shadow=1", "BackColour=&H80000000")
	case "translucent", "opaque": // a box behind each line, drawn in the outline colour
		box := assColor("#000000", 0)
		if st.Background == "translucent" {
			box = assColor("#000000", 0x80)
		}
		parts = append(parts, "BorderStyle=3", "Outline=1", "Shadow=0", "OutlineColour="+box, "BackColour="+box)
	default: // outline
		parts = append(parts, "BorderStyle=1", "Outline=1.5", "Shadow=0", "OutlineColour=&H00000000")
	}
	margin := subtitleMarginBottom
	if st.Position == "raised" {
		margin = subtitleMarginRaised
	}
	return strings.Join(append(parts, fmt.Sprintf("MarginV=%d", margin)), ",")
}

// styledSubtitle reports subtitles that carry their own styling (ASS/SSA), which keep it.
func styledSubtitle(s *SubtitleStream) bool {
	if s.Codec == "ass" || s.Codec == "ssa" {
		return true
	}
	ext := strings.ToLower(filepath.Ext(s.External))
	return ext == ".ass" || ext == ".ssa"
}

// burnStyle is the force_style for burning s in the given style: only plain text subtitles
// are restyled; ASS/SSA keep their look and bitmaps can't be.
func burnStyle(s *SubtitleStream, st SubtitleStyle) string {
	if s == nil || s.IsImage() || isImageFile(s.External) || styledSubtitle(s) {
		return ""
	}
	return st.ForceStyle()
}
