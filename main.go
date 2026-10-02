package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
	"golang.org/x/term"
)

const (
	graphCols   = 60
	graphRows   = 6
	graphPoints = graphCols * 2 // braille gives 2 dots per cell horizontally
	yLabelWidth = 12
	gridWidth   = 80
	maxMetrics  = 5
	xTicks      = 4 // time labels under each graph
)

type config struct {
	Prometheus struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Protocol string `yaml:"protocol"`
	} `yaml:"prometheus"`
	RefreshInterval int     `yaml:"refresh_interval"` // milliseconds
	Range           float64 `yaml:"range"`            // seconds of history to display
	Theme           string  `yaml:"theme"`
	Graphics        string  `yaml:"graphics"` // auto, kitty, iterm2, braille
	// Ranges selected with keys 1-4, e.g. 12h, 30d
	Span1   string  `yaml:"span1"`
	Span2   string  `yaml:"span2"`
	Span3   string  `yaml:"span3"`
	Span4   string  `yaml:"span4"`
	spans   [4]span // parsed from Span1-4
	Metrics []struct {
		Name  string `yaml:"name"`
		Query string `yaml:"query"`
	} `yaml:"metrics"`
}

// span is a selectable range: seconds plus the text shown for it, as written in the config.
type span struct {
	sec   float64
	label string
}

type series struct {
	label  string
	values []float64 // one value per dot column, NaN = no sample
}

type dashboard struct {
	cfg    config
	client *http.Client
	start  time.Time
	th     theme
	gfx    graphicsMode
	points int // samples fetched per series

	mu      sync.Mutex
	rng     span    // history displayed
	end     float64 // unix seconds of the last sample in data
	data    map[string][]series
	version int    // bumped on every data update
	imgKey  string // what the charts on screen were drawn from; redraw images when it changes
}

// chartJob is a chart to be drawn as an image at a cell position.
type chartJob struct {
	row, col int
	vals     [][]float64
	lo, hi   float64
}

// configPath prefers ~/.config/prom-dash/prom-dash.yaml, falling back to the current directory.
func configPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".config", "prom-dash", "prom-dash.yaml")
		// anything but "not found" (e.g. permission denied) is surfaced by loadConfig
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			return p
		}
	}
	return "prom-dash.yaml"
}

func loadConfig(path string) (config, error) {
	var cfg config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Range <= 0 {
		cfg.Range = 300
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 500
	}
	defaults := [4]string{"12h", "24h", "30d", "180d"}
	for i, sp := range [4]string{cfg.Span1, cfg.Span2, cfg.Span3, cfg.Span4} {
		if sp == "" {
			sp = defaults[i]
		}
		v, err := parseSpan(sp)
		if err != nil {
			return cfg, fmt.Errorf("%s: span%d: %w", path, i+1, err)
		}
		cfg.spans[i] = span{v, sp}
	}
	return cfg, nil
}

// parseSpan parses a duration like 12h or 30d into seconds; time.ParseDuration has no day unit.
func parseSpan(sp string) (float64, error) {
	var v float64
	if n, ok := strings.CutSuffix(sp, "d"); ok {
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", sp)
		}
		v = f * 86400
	} else {
		dur, err := time.ParseDuration(sp)
		if err != nil {
			return 0, err
		}
		v = dur.Seconds()
	}
	if v <= 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("duration must be positive: %q", sp)
	}
	return v, nil
}

// formatSpan renders seconds in the largest unit that divides it evenly, e.g. 30d, 12h, 300s.
func formatSpan(sec float64) string {
	for _, u := range []struct {
		n    float64
		unit string
	}{{86400, "d"}, {3600, "h"}, {60, "m"}} {
		if sec >= u.n && math.Mod(sec, u.n) == 0 {
			return fmt.Sprintf("%g%s", sec/u.n, u.unit)
		}
	}
	return fmt.Sprintf("%gs", sec)
}

// timeLayout picks a label format precise enough to tell the x ticks of a range apart.
func timeLayout(rng float64) string {
	switch {
	case rng < 3600:
		return "15:04:05"
	case rng < 86400:
		return "15:04"
	case rng < 7*86400:
		return "01/02 15:04"
	}
	return "2006-01-02"
}

// setRange switches the displayed range and drops data fetched for the old one.
func (d *dashboard) setRange(rng span) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rng == rng {
		return
	}
	d.rng = rng
	clear(d.data) // old samples would be drawn against the new time axis
	d.version++
}

// fetchRange fetches the rng seconds ending at end via query_range, d.points samples per series.
func (d *dashboard) fetchRange(query string, rng, end float64) ([]series, error) {
	step := rng / float64(d.points-1)
	start := end - rng
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	params := url.Values{"query": {query}, "start": {f(start)}, "end": {f(end)}, "step": {f(step)}}
	u := fmt.Sprintf("%s://%s:%d/api/v1/query_range?%s",
		d.cfg.Prometheus.Protocol, d.cfg.Prometheus.Host, d.cfg.Prometheus.Port, params.Encode())

	resp, err := d.client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]any          `json:"values"` // [unix seconds, "value"]
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&body); err != nil {
		return nil, err
	}
	if body.Status != "success" {
		return nil, errors.New("query failed: " + body.Status)
	}

	out := make([]series, 0, len(body.Data.Result))
	for _, r := range body.Data.Result {
		values := make([]float64, d.points)
		for i := range values {
			values[i] = math.NaN()
		}
		for _, p := range r.Values {
			ts, _ := p[0].(float64)
			s, _ := p[1].(string)
			v, err := strconv.ParseFloat(s, 64)
			x := int(math.Round((ts - start) / step))
			if err == nil && x >= 0 && x < d.points && !math.IsInf(v, 0) {
				values[x] = v
			}
		}
		var labels []string
		for _, k := range slices.Sorted(maps.Keys(r.Metric)) {
			if k != "__name__" {
				labels = append(labels, k+"="+r.Metric[k])
			}
		}
		label := strings.Join(labels, ",")
		if label == "" {
			label = "value"
		}
		out = append(out, series{label, values})
	}
	return out, nil
}

func (d *dashboard) collect() {
	d.mu.Lock()
	rng := d.rng.sec
	d.mu.Unlock()
	step := rng / float64(d.points-1)
	// Align to step so samples don't shift between refreshes
	end := math.Floor(float64(time.Now().UnixMilli())/1000/step) * step
	var wg sync.WaitGroup
	for _, m := range d.cfg.Metrics {
		wg.Go(func() {
			s, err := d.fetchRange(m.Query, rng, end)
			// Keep the last good data on failure (errors are not shown, same as the TS version)
			if err != nil {
				return
			}
			d.mu.Lock()
			// The range may have been switched while this request was in flight
			if d.rng.sec == rng {
				d.data[m.Name] = s
				d.end = end
				d.version++
			}
			d.mu.Unlock()
		})
	}
	wg.Wait()
}

func formatValue(name string, v float64) string {
	switch {
	case strings.Contains(name, "bytes"):
		switch {
		case v > 1e6:
			return fmt.Sprintf("%.2f MB/s", v/1e6)
		case v > 1e3:
			return fmt.Sprintf("%.2f KB/s", v/1e3)
		default:
			return fmt.Sprintf("%.2f B/s", v)
		}
	case strings.Contains(name, "packets"):
		return fmt.Sprintf("%.0f pkt/s", v)
	}
	return fmt.Sprintf("%.2f", v)
}

type style struct {
	fg   color.RGBA
	bold bool
}

type cell struct {
	ch   rune
	st   style
	skip bool // covered by an image: never written, so the image isn't overwritten
}

// screen is a full-frame buffer; every frame redraws all cells except image areas, so no clear is needed.
type screen struct {
	w, h  int
	cells [][]cell
}

func newScreen(w, h int, plain style) *screen {
	s := &screen{w: w, h: h, cells: make([][]cell, h)}
	for r := range s.cells {
		s.cells[r] = make([]cell, w)
		for c := range s.cells[r] {
			s.cells[r][c] = cell{ch: ' ', st: plain}
		}
	}
	return s
}

// put writes text at (row, col), clipping at the screen edges.
// ponytail: assumes every rune is 1 column wide; wide chars (CJK, emoji) in labels will misalign.
func (s *screen) put(row, col int, text string, st style) {
	if row < 0 || row >= s.h {
		return
	}
	for _, ch := range text {
		if col >= 0 && col < s.w {
			s.cells[row][col].ch, s.cells[row][col].st = ch, st
		}
		col++
	}
}

func (s *screen) skipArea(row, col, cols, rows int) {
	for r := row; r < min(row+rows, s.h); r++ {
		for c := max(col, 0); c < min(col+cols, s.w); c++ {
			s.cells[r][c].skip = true
		}
	}
}

// bytes serializes the screen as truecolor SGR, jumping over skipped cells.
func (s *screen) bytes(b *bytes.Buffer, bg color.RGBA) {
	for r, row := range s.cells {
		var cur style
		moved := false
		for c, cl := range row {
			if cl.skip {
				moved = false
				continue
			}
			if !moved {
				fmt.Fprintf(b, "\x1b[%d;%dH", r+1, c+1)
				moved = true
			}
			if c == 0 || cl.st != cur || row[c-1].skip {
				cur = cl.st
				fmt.Fprintf(b, "\x1b[0;48;2;%d;%d;%d;38;2;%d;%d;%d", bg.R, bg.G, bg.B, cur.fg.R, cur.fg.G, cur.fg.B)
				if cur.bold {
					b.WriteString(";1")
				}
				b.WriteByte('m')
			}
			b.WriteRune(cl.ch)
		}
	}
	b.WriteString("\x1b[0m")
}

func (d *dashboard) draw(w, h int) []byte {
	plain, muted, bold := style{fg: d.th.fg}, style{fg: d.th.muted}, style{fg: d.th.fg, bold: true}
	s := newScreen(w, h, plain)
	el := int(time.Since(d.start).Seconds())
	s.put(1, 2, fmt.Sprintf("%02d:%02d:%02d", el/3600%100, el/60%60, el%60), bold)

	d.mu.Lock()
	defer d.mu.Unlock()

	total := 0
	for _, ss := range d.data {
		for _, sr := range ss {
			for _, v := range sr.values {
				if !math.IsNaN(v) {
					total++
				}
			}
		}
	}
	score := fmt.Sprintf("%d DR %d", total, len(d.data))
	s.put(1, w-2-len(score), score, bold)

	jobs := d.drawGrid(s, 3, max((w-gridWidth)/2, 0))

	avg := 0
	if len(d.data) > 0 {
		avg = total / len(d.data)
	}
	stats := fmt.Sprintf("%d | %d", total, avg)
	s.put(h-3, (w-len(stats))/2, stats, bold)
	var spans []string
	for i, sp := range d.cfg.spans {
		spans = append(spans, fmt.Sprintf("%d:%s", i+1, sp.label))
	}
	s.put(h-1, 0, "--- | "+strings.Join(spans, " ")+" | Press q or ESC to quit", muted)

	var b bytes.Buffer
	b.WriteString("\x1b[?2026h") // synchronized output: no flicker on terminals that support it
	s.bytes(&b, d.th.bg)
	// Images only change with data or layout; re-sending them every frame would be wasteful
	if key := fmt.Sprint(d.version, w, h); d.gfx != gfxBraille && key != d.imgKey {
		d.imgKey = key
		// Remove charts whose slot is no longer drawn as an image (e.g. fell back to braille)
		if d.gfx == gfxKitty {
			for id := len(jobs) + 1; id <= maxMetrics; id++ {
				b.WriteString(kittyDelete(id))
			}
		}
		bg := d.th.bg
		for i, j := range jobs {
			// Blank the area first: skipped cells are never redrawn, so text left there
			// (e.g. from a previous terminal size) would otherwise stay around the image
			for r := range graphRows {
				fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[0;48;2;%d;%d;%dm%s", j.row+r+1, j.col+1, bg.R, bg.G, bg.B, strings.Repeat(" ", graphCols))
			}
			b.WriteString("\x1b[0m")
			img := renderChart(j.vals, graphCols, graphRows, j.lo, j.hi, d.th)
			b.WriteString(placeImage(d.gfx, img, j.row, j.col, graphCols, graphRows, i+1))
		}
	}
	b.WriteString("\x1b[?2026l")
	return b.Bytes()
}

func (d *dashboard) drawGrid(s *screen, line, left int) []chartJob {
	plain, muted, bold := style{fg: d.th.fg}, style{fg: d.th.muted}, style{fg: d.th.accent, bold: true}
	var jobs []chartJob
	shown := 0
	for _, m := range d.cfg.Metrics {
		ss, ok := d.data[m.Name]
		if !ok || shown == maxMetrics {
			continue
		}
		shown++

		s.put(line, left, m.Name, bold)
		s.put(line, left+len(m.Name), fmt.Sprintf("  (last %s)", d.rng.label), muted)
		line++

		lo, hi := math.Inf(1), math.Inf(-1)
		vals := make([][]float64, len(ss))
		for i, sr := range ss {
			vals[i] = sr.values
			for _, v := range sr.values {
				if !math.IsNaN(v) {
					lo, hi = min(lo, v), max(hi, v)
				}
			}
		}
		if math.IsInf(lo, 1) {
			s.put(line, left, "No data", muted)
			line += 2
			continue
		}
		// Center a flat line instead of dividing by zero
		if lo == hi {
			pad := math.Abs(hi) * 0.1
			if pad == 0 {
				pad = 1
			}
			lo, hi = lo-pad, hi+pad
		}

		for r := range graphRows {
			label, tick := "", "│"
			switch r {
			case 0:
				label, tick = formatValue(m.Name, hi), "┤"
			case graphRows - 1:
				label, tick = formatValue(m.Name, lo), "┤"
			}
			s.put(line+r, left, fmt.Sprintf("%*s", yLabelWidth, label), plain)
			s.put(line+r, left+yLabelWidth+1, tick, muted)
		}
		chartCol := left + yLabelWidth + 2
		// An image scrolls the screen if it doesn't fit, so fall back to braille near the bottom
		if d.gfx != gfxBraille && line+graphRows < s.h && chartCol+graphCols <= s.w {
			s.skipArea(line, chartCol, graphCols, graphRows)
			jobs = append(jobs, chartJob{line, chartCol, vals, lo, hi})
		} else {
			for r, row := range plotBraille(vals, graphCols, graphRows, lo, hi) {
				for c, bc := range row {
					st := plain
					if bc.series >= 0 {
						st.fg = d.th.seriesColor(bc.series)
					}
					s.put(line+r, chartCol+c, string(bc.ch), st)
				}
			}
		}
		line += graphRows
		axis := []rune("└" + strings.Repeat("─", graphCols))
		layout := timeLayout(d.rng.sec)
		for i := range xTicks {
			c := i * (graphCols - 1) / (xTicks - 1)
			axis[1+c] = '┬'
			t := time.Unix(int64(d.end-d.rng.sec*float64(xTicks-1-i)/float64(xTicks-1)), 0)
			lbl := t.Format(layout)
			s.put(line+1, min(max(chartCol+c-len(lbl)/2, left), chartCol+graphCols-len(lbl)), lbl, muted)
		}
		s.put(line, left+yLabelWidth+1, string(axis), muted)
		line += 2

		// Legend: one entry per series with its latest value, as many as fit on one line
		col := left
		for i, sr := range ss {
			latest := "N/A"
			for j := len(sr.values) - 1; j >= 0; j-- {
				if !math.IsNaN(sr.values[j]) {
					latest = formatValue(m.Name, sr.values[j])
					break
				}
			}
			lbl := sr.label
			if utf8.RuneCountInString(lbl) > 40 {
				lbl = string([]rune(lbl)[:40])
			}
			text := fmt.Sprintf("● %s %s", lbl, latest)
			n := utf8.RuneCountInString(text)
			if col-left+n > 78 {
				continue
			}
			s.put(line, col, text, style{fg: d.th.seriesColor(i)})
			col += n + 2
		}
		line++
	}
	if shown == 0 {
		s.put(line, left, "Waiting for metrics...", plain)
	}
	return jobs
}

func main() {
	cfg, err := loadConfig(configPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	th, err := lookupTheme(cfg.Theme)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	gfx, err := detectGraphics(cfg.Graphics)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	points := graphPoints
	if gfx != gfxBraille {
		points = graphCols * cellPxW / 2 // one sample per 2 px
	}
	d := &dashboard{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		start:  time.Now(),
		th:     th,
		gfx:    gfx,
		points: points,
		rng:    span{cfg.Range, formatSpan(cfg.Range)},
		data:   map[string][]series{},
	}

	in := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdin must be a terminal (use docker run -it):", err)
		os.Exit(1)
	}
	// Window title, alternate screen, hidden cursor
	os.Stdout.WriteString("\x1b]0;Prometheus Dashboard\x07\x1b[?1049h\x1b[?25l")
	defer func() {
		os.Stdout.WriteString("\x1b[?25h\x1b[?1049l")
		term.Restore(in, oldState)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		buf := make([]byte, 16)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				quit <- syscall.SIGHUP
				return
			}
			if k := buf[0]; k >= '1' && k <= '4' {
				d.setRange(cfg.spans[k-'1'])
				go d.collect() // don't wait for the next tick to show the new range
				continue
			}
			// q, Ctrl+C, or a lone ESC (escape sequences like arrow keys arrive as one longer read)
			if buf[0] == 'q' || buf[0] == 0x03 || (n == 1 && buf[0] == 0x1b) {
				quit <- syscall.SIGINT
				return
			}
		}
	}()

	go func() {
		d.collect()
		for range time.Tick(time.Duration(cfg.RefreshInterval) * time.Millisecond) {
			d.collect()
		}
	}()

	out := int(os.Stdout.Fd())
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		if w, h, err := term.GetSize(out); err == nil {
			os.Stdout.Write(d.draw(w, h))
		}
		select {
		case <-quit:
			return
		case <-ticker.C:
		}
	}
}
