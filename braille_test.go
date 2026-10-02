package main

import (
	"math"
	"reflect"
	"testing"
)

func chars(series [][]float64, cols, rows int) []string {
	var out []string
	for _, row := range plotBraille(series, cols, rows, 0, 1) {
		s := ""
		for _, c := range row {
			s += string(c.ch)
		}
		out = append(out, s)
	}
	return out
}

func TestPlotBraille(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		name       string
		series     [][]float64
		cols, rows int
		want       []string
	}{
		{"flat line at min sits on the bottom dot row", [][]float64{{0, 0, 0, 0}}, 2, 2, []string{"  ", "⣀⣀"}},
		{"rise is connected vertically", [][]float64{{0, 1}}, 1, 1, []string{"⣸"}},
		{"NaN breaks the line", [][]float64{{0, nan, 1, 1}}, 2, 1, []string{"⡀⠉"}},
	}
	for _, tt := range tests {
		if got := chars(tt.series, tt.cols, tt.rows); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestPlotBrailleOwner(t *testing.T) {
	cells := plotBraille([][]float64{{0, 0}, {1, 1}}, 1, 1, 0, 1)
	if want := (brailleCell{'⣉', 1}); cells[0][0] != want {
		t.Errorf("got %+v, want %+v", cells[0][0], want)
	}
}
