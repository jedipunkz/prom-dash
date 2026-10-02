package main

import "math"

// A braille cell is 2x4 dots. dotBits[y][x] is the bit for the dot at (x, y) within a cell.
var dotBits = [4][2]rune{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

type brailleCell struct {
	ch     rune
	series int // index of the series drawn last in this cell, -1 if empty
}

// plotBraille plots line charts into a cols x rows grid of braille cells (cols*2 x rows*4 dots).
// Each series holds one value per dot column; NaN is a gap and breaks the line.
func plotBraille(series [][]float64, cols, rows int, lo, hi float64) [][]brailleCell {
	height := rows * 4
	bits := make([][]rune, rows)
	owner := make([][]int, rows)
	for r := range rows {
		bits[r] = make([]rune, cols)
		owner[r] = make([]int, cols)
		for c := range owner[r] {
			owner[r][c] = -1
		}
	}
	toY := func(v float64) int {
		y := int(math.Round((hi - v) / (hi - lo) * float64(height-1)))
		return min(max(y, 0), height-1)
	}

	for s, values := range series {
		prev := -1
		for x, v := range values[:min(len(values), cols*2)] {
			if math.IsNaN(v) {
				prev = -1
				continue
			}
			y := toY(v)
			// Fill the vertical span from the previous point so steep changes stay connected
			from, to := y, y
			if prev >= 0 {
				from, to = min(prev, y), max(prev, y)
			}
			for yy := from; yy <= to; yy++ {
				bits[yy/4][x/2] |= dotBits[yy%4][x%2]
				owner[yy/4][x/2] = s
			}
			prev = y
		}
	}

	out := make([][]brailleCell, rows)
	for r := range rows {
		out[r] = make([]brailleCell, cols)
		for c, b := range bits[r] {
			ch := ' '
			if b != 0 {
				ch = 0x2800 + b
			}
			out[r][c] = brailleCell{ch, owner[r][c]}
		}
	}
	return out
}
