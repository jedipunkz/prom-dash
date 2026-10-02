package main

import "testing"

func TestRenderChartAlignsWithLabels(t *testing.T) {
	th := themes["tokyonight"]
	img := renderChart([][]float64{{1, 1}, {0, 0}}, 2, 2, 0, 1, th)
	// hi sits at the center of the top cell row, lo at the center of the bottom one
	if got := img.RGBAAt(5, cellPxH/2); got != th.seriesColor(0) {
		t.Errorf("hi line: got %v, want %v", got, th.seriesColor(0))
	}
	if got := img.RGBAAt(5, 2*cellPxH-cellPxH/2); got != th.seriesColor(1) {
		t.Errorf("lo line: got %v, want %v", got, th.seriesColor(1))
	}
}

func TestDetectGraphics(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "WezTerm")
	t.Setenv("TMUX", "")
	if got, _ := detectGraphics("auto"); got != gfxITerm2 {
		t.Errorf("WezTerm: got %v, want iterm2", got)
	}
	t.Setenv("TMUX", "/tmp/tmux-501/default,1,0")
	if got, _ := detectGraphics("auto"); got != gfxBraille {
		t.Errorf("tmux: got %v, want braille", got)
	}
	if _, err := detectGraphics("sixel"); err == nil {
		t.Error("unknown mode: want error")
	}
	if _, err := lookupTheme("doracula"); err == nil {
		t.Error("unknown theme: want error")
	}
}
