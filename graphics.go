package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strings"
)

type graphicsMode int

const (
	gfxBraille graphicsMode = iota
	gfxKitty                // kitty graphics protocol: kitty, Ghostty, WezTerm (also inside herdr)
	gfxITerm2               // iTerm2 inline images: iTerm2
)

// Charts are rendered at a fixed pixel size per cell and scaled by the terminal.
// ponytail: fixed size, query the real cell size (TIOCGWINSZ) if charts look blurry.
const (
	cellPxW = 16
	cellPxH = 32
)

func detectGraphics(setting string) (graphicsMode, error) {
	switch setting {
	case "kitty":
		return gfxKitty, nil
	case "iterm2":
		return gfxITerm2, nil
	case "braille":
		return gfxBraille, nil
	case "", "auto":
	default:
		return gfxBraille, fmt.Errorf("unknown graphics %q (available: auto, kitty, iterm2, braille)", setting)
	}
	// tmux and zellij drop image escapes. herdr keeps the outer terminal's TERM_PROGRAM
	// and renders only kitty graphics, which is why WezTerm maps to kitty, not iTerm2.
	if os.Getenv("TMUX") != "" || os.Getenv("ZELLIJ") != "" {
		return gfxBraille, nil
	}
	switch tp := strings.ToLower(os.Getenv("TERM_PROGRAM")); {
	case os.Getenv("KITTY_WINDOW_ID") != "", os.Getenv("TERM") == "xterm-kitty",
		os.Getenv("TERM") == "xterm-ghostty", tp == "ghostty", tp == "wezterm":
		return gfxKitty, nil
	case tp == "iterm.app":
		return gfxITerm2, nil
	}
	return gfxBraille, nil
}

// renderChart draws line charts into a cols x rows cell area.
// Values map so hi sits at the center of the top cell row and lo at the center of the bottom
// one, which keeps them aligned with the Y-axis labels printed beside the chart.
func renderChart(series [][]float64, cols, rows int, lo, hi float64, th theme) *image.RGBA {
	w, h := cols*cellPxW, rows*cellPxH
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = th.bg.R, th.bg.G, th.bg.B, 255
	}
	toY := func(v float64) int {
		return cellPxH/2 + int(math.Round((hi-v)/(hi-lo)*float64(h-cellPxH)))
	}

	// Dashed grid lines at hi, mid, lo
	for _, v := range []float64{hi, (hi + lo) / 2, lo} {
		y := toY(v)
		for x := 0; x < w; x++ {
			if x%8 < 4 {
				img.SetRGBA(x, y, th.muted)
			}
		}
	}

	const thick = 3
	for s, values := range series {
		c := th.seriesColor(s)
		px, py := -1, -1
		for i, v := range values {
			if math.IsNaN(v) {
				px = -1
				continue
			}
			x := 0
			if len(values) > 1 {
				x = i * (w - 1) / (len(values) - 1)
			}
			y := toY(v)
			if px < 0 {
				px, py = x, y
			}
			drawLine(img, px, py, x, y, thick, c)
			px, py = x, y
		}
	}
	return img
}

// drawLine draws a Bresenham line stamping thick x thick squares.
func drawLine(img *image.RGBA, x0, y0, x1, y1, thick int, c color.RGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := sign(x1-x0), sign(y1-y0)
	err := dx + dy
	for {
		for oy := -thick / 2; oy <= thick/2; oy++ {
			for ox := -thick / 2; ox <= thick/2; ox++ {
				if image.Pt(x0+ox, y0+oy).In(img.Rect) {
					img.SetRGBA(x0+ox, y0+oy, c)
				}
			}
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		if e2 := 2 * err; e2 >= dy {
			err += dy
			x0 += sx
		} else {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int  { return max(v, -v) }
func sign(v int) int { return min(max(v, -1), 1) }

// placeImage returns the escape sequence that draws img at (row, col) over cols x rows cells.
// id identifies the chart slot so a redraw replaces its image instead of stacking another.
func placeImage(mode graphicsMode, img image.Image, row, col, cols, rows, id int) string {
	var buf bytes.Buffer
	png.Encode(&buf, img) // writing to a bytes.Buffer cannot fail
	data := base64.StdEncoding.EncodeToString(buf.Bytes())

	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[%d;%dH", row+1, col+1)
	if mode == gfxITerm2 {
		fmt.Fprintf(&b, "\x1b]1337;File=inline=1;size=%d;width=%d;height=%d;preserveAspectRatio=0:%s\x07",
			buf.Len(), cols, rows, data)
		return b.String()
	}
	// kitty: payload is sent in 4096-byte chunks; C=1 keeps the cursor still, q=2 silences replies.
	// Delete the old image first: not every terminal replaces a placement re-sent under the same ids.
	b.WriteString(kittyDelete(id))
	for i := 0; i < len(data); i += 4096 {
		more := 0
		if i+4096 < len(data) {
			more = 1
		}
		chunk := data[i:min(i+4096, len(data))]
		if i == 0 {
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,i=%d,p=1,c=%d,r=%d,C=1,q=2,m=%d;%s\x1b\\", id, cols, rows, more, chunk)
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return b.String()
}

// kittyDelete removes the kitty image with the given id and frees its data.
func kittyDelete(id int) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}
