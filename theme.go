package main

import (
	"fmt"
	"image/color"
	"maps"
	"slices"
	"strings"
)

type theme struct {
	bg, fg, muted, accent color.RGBA
	series                []color.RGBA
}

func hex(s string) color.RGBA {
	c := color.RGBA{A: 255}
	fmt.Sscanf(s, "#%02x%02x%02x", &c.R, &c.G, &c.B)
	return c
}

func hexes(ss ...string) []color.RGBA {
	out := make([]color.RGBA, len(ss))
	for i, s := range ss {
		out[i] = hex(s)
	}
	return out
}

// Dark variants of each scheme, taken from the upstream palettes.
var themes = map[string]theme{
	"retro": {hex("#bab1a1"), hex("#000000"), hex("#4a453d"), hex("#000000"),
		hexes("#000000", "#a83232", "#2a4d8f", "#7d3c7d", "#2f6b2f")},
	"tokyonight": {hex("#1a1b26"), hex("#c0caf5"), hex("#565f89"), hex("#7aa2f7"),
		hexes("#7aa2f7", "#9ece6a", "#bb9af7", "#ff9e64", "#7dcfff", "#f7768e", "#e0af68")},
	"kanagawa-wave": {hex("#1f1f28"), hex("#dcd7ba"), hex("#727169"), hex("#7e9cd8"),
		hexes("#7e9cd8", "#98bb6c", "#957fb8", "#ffa066", "#7aa89f", "#e46876", "#e6c384")},
	"solarized": {hex("#002b36"), hex("#839496"), hex("#586e75"), hex("#268bd2"),
		hexes("#268bd2", "#859900", "#d33682", "#cb4b16", "#2aa198", "#dc322f", "#b58900")},
	"dracula": {hex("#282a36"), hex("#f8f8f2"), hex("#6272a4"), hex("#bd93f9"),
		hexes("#bd93f9", "#50fa7b", "#ff79c6", "#ffb86c", "#8be9fd", "#ff5555", "#f1fa8c")},
	"gruvbox": {hex("#282828"), hex("#ebdbb2"), hex("#928374"), hex("#83a598"),
		hexes("#83a598", "#b8bb26", "#d3869b", "#fe8019", "#8ec07c", "#fb4934", "#fabd2f")},
}

func lookupTheme(name string) (theme, error) {
	if name == "" {
		name = "retro"
	}
	t, ok := themes[name]
	if !ok {
		return t, fmt.Errorf("unknown theme %q (available: %s)", name,
			strings.Join(slices.Sorted(maps.Keys(themes)), ", "))
	}
	return t, nil
}

func (t theme) seriesColor(i int) color.RGBA {
	return t.series[i%len(t.series)]
}
