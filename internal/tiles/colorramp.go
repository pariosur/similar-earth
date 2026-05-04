package tiles

// ColorRamp is a 256-entry look-up table mapping a quantised similarity
// score (0-255) to an RGBA colour. Index 0 corresponds to score ~0,
// index 255 to score ~1.
type ColorRamp [256][4]uint8

// DefaultRamp returns a heat-style colour ramp: transparent at low scores,
// yellow for moderate similarity, orange for good, red/crimson for best match.
// Designed for overlay on a dark basemap.
func DefaultRamp() ColorRamp {
	var ramp ColorRamp

	for i := 0; i < 256; i++ {
		var r, g, b, a uint8

		switch {
		case i <= 140:
			// 0.00-0.55: below threshold, fully transparent.
			r, g, b, a = 0, 0, 0, 0

		case i <= 166:
			// 0.55-0.65: faint yellow. Marginal match.
			f := float64(i-141) / float64(166-141)
			r = lerp8(200, 240, f)
			g = lerp8(180, 200, f)
			b = lerp8(50, 30, f)
			a = lerp8(60, 140, f)

		case i <= 204:
			// 0.65-0.80: yellow -> orange. Good match.
			f := float64(i-167) / float64(204-167)
			r = lerp8(240, 245, f)
			g = lerp8(200, 120, f)
			b = lerp8(30, 20, f)
			a = lerp8(140, 210, f)

		case i <= 240:
			// 0.80-0.94: orange -> red. Strong match.
			f := float64(i-205) / float64(240-205)
			r = lerp8(245, 220, f)
			g = lerp8(120, 40, f)
			b = lerp8(20, 20, f)
			a = lerp8(210, 240, f)

		default:
			// 0.94-1.0: deep red/crimson. Top match.
			f := float64(i-241) / float64(255-241)
			r = lerp8(220, 180, f)
			g = lerp8(40, 15, f)
			b = lerp8(20, 30, f)
			a = lerp8(240, 255, f)
		}

		ramp[i] = [4]uint8{r, g, b, a}
	}

	return ramp
}

// lerp8 linearly interpolates between two uint8 values.
func lerp8(a, b uint8, t float64) uint8 {
	v := float64(a) + (float64(b)-float64(a))*t
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
