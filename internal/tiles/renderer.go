package tiles

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/pariosur/tierraai/internal/grid"
	"github.com/pariosur/tierraai/internal/similarity"
)

const tileSize = 256

// Renderer converts similarity scores into 256x256 RGBA PNG tiles.
type Renderer struct {
	Ramp ColorRamp
}

// NewRenderer creates a Renderer with the default Magma-inspired ramp.
func NewRenderer() *Renderer {
	ramp := DefaultRamp()
	return &Renderer{Ramp: ramp}
}

// RenderTile produces a 256x256 RGBA PNG for the given web-mercator tile
// coordinates. result must contain the full row-major score grid.
func (r *Renderer) RenderTile(result *similarity.QueryResult, z, x, y int, g *grid.Grid) ([]byte, error) {
	west, south, east, north := TileBounds(z, x, y)

	img := image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize))

	lngStep := (east - west) / float64(tileSize)
	latStep := (north - south) / float64(tileSize)

	for py := 0; py < tileSize; py++ {
		// Pixel row 0 is the top of the tile (north).
		lat := north - (float64(py)+0.5)*latStep
		for px := 0; px < tileSize; px++ {
			lng := west + (float64(px)+0.5)*lngStep

			row, col, ok := g.LatLngToRowCol(lat, lng)
			if !ok {
				img.SetNRGBA(px, py, color.NRGBA{})
				continue
			}

			idx := row*result.Width + col
			if idx < 0 || idx >= len(result.Scores) {
				img.SetNRGBA(px, py, color.NRGBA{})
				continue
			}

			score := result.Scores[idx]
			ci := int(score * 255)
			if ci > 255 {
				ci = 255
			}
			if ci < 0 {
				ci = 0
			}

			c := r.Ramp[ci]
			img.SetNRGBA(px, py, color.NRGBA{R: c[0], G: c[1], B: c[2], A: c[3]})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderTileFromScores renders a 256x256 PNG from pre-computed similarity scores.
// Used for COG-refined tiles where scores are already computed by the Python service.
func (r *Renderer) RenderTileFromScores(scores []float32) ([]byte, error) {
	if len(scores) != tileSize*tileSize {
		return nil, fmt.Errorf("expected %d scores, got %d", tileSize*tileSize, len(scores))
	}

	img := image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize))

	for py := 0; py < tileSize; py++ {
		for px := 0; px < tileSize; px++ {
			score := scores[py*tileSize+px]
			ci := int(score * 255)
			if ci > 255 {
				ci = 255
			}
			if ci < 0 {
				ci = 0
			}
			c := r.Ramp[ci]
			img.SetNRGBA(px, py, color.NRGBA{R: c[0], G: c[1], B: c[2], A: c[3]})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TileBounds converts web-mercator tile coordinates (z/x/y) to a
// geographic bounding box (west, south, east, north) in degrees.
func TileBounds(z, x, y int) (west, south, east, north float64) {
	n := math.Pow(2, float64(z))
	west = float64(x)/n*360.0 - 180.0
	east = float64(x+1)/n*360.0 - 180.0
	north = tileLatDeg(y, z)
	south = tileLatDeg(y+1, z)
	return
}

// tileLatDeg converts a tile y-index at zoom z to latitude in degrees.
func tileLatDeg(y, z int) float64 {
	n := math.Pi - 2.0*math.Pi*float64(y)/math.Pow(2, float64(z))
	return 180.0 / math.Pi * math.Atan(math.Sinh(n))
}
