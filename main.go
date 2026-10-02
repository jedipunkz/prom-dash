package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
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
	bgColor     = "48;2;186;177;161" // beige background (truecolor SGR)
)

// SGR foreground codes per series: black, red, blue, magenta, green
var seriesColors = []int{30, 31, 34, 35, 32}

type config struct {
	Prometheus struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Protocol string `yaml:"protocol"`
	} `yaml:"prometheus"`
	RefreshInterval int     `yaml:"refresh_interval"` // milliseconds
	Range           float64 `yaml:"range"`            // seconds of history to display
	Metrics         []struct {
		Name  string `yaml:"name"`
		Query string `yaml:"query"`
	} `yaml:"metrics"`
}

type series struct {
	label  string
	values []float64 // one value per dot column, NaN = no sample
}

type dashboard struct {
	cfg    config
	client *http.Client
	start  time.Time

	mu   sync.Mutex
	data map[string][]series
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
	return cfg, nil
}

// fetchRange fetches the whole display window via query_range, one sample per dot column.
func (d *dashboard) fetchRange(query string) ([]series, error) {
	step := d.cfg.Range / (graphPoints - 1)
	// Align to step so samples don't shift between refreshes
	end := math.Floor(float64(time.Now().UnixMilli())/1000/step) * step
	start := end - step*(graphPoints-1)
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
		values := make([]float64, graphPoints)
		for i := range values {
			values[i] = math.NaN()
		}
		for _, p := range r.Values {
			ts, _ := p[0].(float64)
			s, _ := p[1].(string)
			v, err := strconv.ParseFloat(s, 64)
			x := int(math.Round((ts - start) / step))
			if err == nil && x >= 0 && x < graphPoints && !math.IsInf(v, 0) {
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
	var wg sync.WaitGroup
	for _, m := range d.cfg.Metrics {
		wg.Go(func() {
			s, err := d.fetchRange(m.Query)
			// Keep the last good data on failure (errors are not shown, same as the TS version)
			if err != nil {
				return
			}
			d.mu.Lock()
			d.data[m.Name] = s
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
	fg   int // SGR foreground code
	bold bool
}

var (
	plain = style{fg: 30}
	bold  = style{fg: 30, bold: true}
)

type cell struct {
	ch rune
	st style
}

// screen is a full-frame buffer; every frame redraws all cells so no clear is needed.
type screen struct {
	w, h  int
	cells [][]cell
}

func newScreen(w, h int) *screen {
	s := &screen{w: w, h: h, cells: make([][]cell, h)}
	for r := range s.cells {
		s.cells[r] = make([]cell, w)
		for c := range s.cells[r] {
			s.cells[r][c] = cell{' ', plain}
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
			s.cells[row][col] = cell{ch, st}
		}
		col++
	}
}

func (s *screen) bytes() []byte {
	var b bytes.Buffer
	b.WriteString("\x1b[?2026h") // synchronized output: no flicker on terminals that support it
	for r, row := range s.cells {
		fmt.Fprintf(&b, "\x1b[%d;1H", r+1)
		var cur style
		for c, cl := range row {
			if c == 0 || cl.st != cur {
				cur = cl.st
				fmt.Fprintf(&b, "\x1b[0;%s;%d", bgColor, cur.fg)
				if cur.bold {
					b.WriteString(";1")
				}
				b.WriteByte('m')
			}
			b.WriteRune(cl.ch)
		}
	}
	b.WriteString("\x1b[0m\x1b[?2026l")
	return b.Bytes()
}

func (d *dashboard) draw(w, h int) []byte {
	s := newScreen(w, h)
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

	d.drawGrid(s, 3, max((w-gridWidth)/2, 0))

	avg := 0
	if len(d.data) > 0 {
		avg = total / len(d.data)
	}
	stats := fmt.Sprintf("%d | %d", total, avg)
	s.put(h-3, (w-len(stats))/2, stats, bold)
	s.put(h-1, 0, "--- | Press q or ESC to quit", plain)
	return s.bytes()
}

func (d *dashboard) drawGrid(s *screen, line, left int) {
	shown := 0
	for _, m := range d.cfg.Metrics {
		ss, ok := d.data[m.Name]
		if !ok || shown == maxMetrics {
			continue
		}
		shown++

		s.put(line, left, m.Name, bold)
		s.put(line, left+len(m.Name), fmt.Sprintf("  (last %gs)", d.cfg.Range), plain)
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
			s.put(line, left, "No data", plain)
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

		for r, row := range plotBraille(vals, graphCols, graphRows, lo, hi) {
			label, tick := "", "│"
			switch r {
			case 0:
				label, tick = formatValue(m.Name, hi), "┤"
			case graphRows - 1:
				label, tick = formatValue(m.Name, lo), "┤"
			}
			s.put(line, left, fmt.Sprintf("%*s %s", yLabelWidth, label, tick), plain)
			for c, bc := range row {
				st := plain
				if bc.series >= 0 {
					st.fg = seriesColors[bc.series%len(seriesColors)]
				}
				s.put(line, left+yLabelWidth+2+c, string(bc.ch), st)
			}
			line++
		}
		s.put(line, left+yLabelWidth+1, "└"+strings.Repeat("─", graphCols), plain)
		line++

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
			s.put(line, col, text, style{fg: seriesColors[i%len(seriesColors)]})
			col += n + 2
		}
		line++
	}
	if shown == 0 {
		s.put(line, left, "Waiting for metrics...", plain)
	}
}

func main() {
	cfg, err := loadConfig("prometheus-dash.yaml")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	d := &dashboard{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		start:  time.Now(),
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
