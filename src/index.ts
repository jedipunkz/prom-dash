import blessed from 'blessed';
import * as fs from 'fs';
import * as yaml from 'js-yaml';
import * as path from 'path';
import { plotBraille } from './braille';

const GRAPH_COLS = 60;
const GRAPH_ROWS = 6;
const GRAPH_POINTS = GRAPH_COLS * 2; // braille gives 2 dots per cell horizontally
const Y_LABEL_WIDTH = 12;
const SERIES_COLORS = ['black', 'red', 'blue', 'magenta', 'green'];

interface Config {
  prometheus: {
    host: string;
    port: number;
    protocol: string;
  };
  refresh_interval: number;
  range?: number; // seconds of history to display
  metrics: Array<{
    name: string;
    query: string;
  }>;
}

interface Series {
  label: string;
  values: Array<number | null>; // one value per dot column, null = no sample
}

class PrometheusDash {
  private screen: blessed.Widgets.Screen;
  private config: Config;
  private startTime: number;
  private metricSeries: Map<string, Series[]> = new Map();

  constructor() {
    // Load config
    const configPath = path.join(process.cwd(), 'prom-dash.yaml');
    this.config = yaml.load(fs.readFileSync(configPath, 'utf8')) as Config;
    this.startTime = Date.now();

    // Create screen
    this.screen = blessed.screen({
      smartCSR: true,
      fullUnicode: true,
    });

    this.screen.title = 'Prometheus Dashboard';
    this.setupUI();
    this.startMetricsCollection();

    // Quit on Escape, q, or Control-C
    this.screen.key(['escape', 'q', 'C-c'], () => {
      return process.exit(0);
    });
  }

  private setupUI() {
    // Background
    const bg = blessed.box({
      top: 0,
      left: 0,
      width: '100%',
      height: '100%',
      style: {
        bg: '#bab1a1', // Beige/tan background
      },
    });
    this.screen.append(bg);

    // Timer box
    const timer = blessed.box({
      top: 1,
      left: 2,
      width: 12,
      height: 1,
      content: '00:00:00',
      tags: true,
      style: {
        fg: 'black',
        bg: '#bab1a1',
        bold: true,
      },
    });
    this.screen.append(timer);

    // Score box
    const score = blessed.box({
      top: 1,
      right: 2,
      width: 20,
      height: 1,
      content: '0 DR 0',
      align: 'right',
      tags: true,
      style: {
        fg: 'black',
        bg: '#bab1a1',
        bold: true,
      },
    });
    this.screen.append(score);

    // Main grid for metrics visualization
    const grid = blessed.box({
      top: 3,
      left: 'center',
      width: 80,
      height: 45,
      tags: true,
      style: {
        bg: '#bab1a1',
        fg: 'black',
      },
    });
    this.screen.append(grid);

    // Bottom stats
    const stats = blessed.box({
      bottom: 2,
      left: 'center',
      width: 50,
      height: 1,
      content: '0 | 0',
      align: 'center',
      tags: true,
      style: {
        fg: 'black',
        bg: '#bab1a1',
        bold: true,
      },
    });
    this.screen.append(stats);

    // Status bar at the very bottom
    const statusBar = blessed.box({
      bottom: 0,
      left: 0,
      width: '100%',
      height: 1,
      content: '--- | Press q or ESC to quit',
      style: {
        fg: 'black',
        bg: '#bab1a1',
      },
    });
    this.screen.append(statusBar);

    // Update timer every second
    setInterval(() => {
      const elapsed = Date.now() - this.startTime;
      const seconds = Math.floor(elapsed / 1000);
      const minutes = Math.floor(seconds / 60);
      const hours = Math.floor(minutes / 60);

      const timeStr = `${String(hours % 100).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`;
      timer.setContent(timeStr);
      this.screen.render();
    }, 1000);

    // Update UI based on metrics
    setInterval(() => {
      this.updateGrid(grid);
      this.updateScore(score);
      this.updateStats(stats);
      this.screen.render();
    }, 100);
  }

  private formatValue(name: string, value: number): string {
    // Format bytes as KB/s or MB/s
    if (name.includes('bytes')) {
      if (value > 1000000) {
        return `${(value / 1000000).toFixed(2)} MB/s`;
      } else if (value > 1000) {
        return `${(value / 1000).toFixed(2)} KB/s`;
      } else {
        return `${value.toFixed(2)} B/s`;
      }
    }
    // Format packets
    if (name.includes('packets')) {
      return `${value.toFixed(0)} pkt/s`;
    }
    // Default formatting
    return value.toFixed(2);
  }

  private updateGrid(grid: blessed.Widgets.BoxElement) {
    const range = this.config.range ?? 300;
    let content = '';

    const metricsToDisplay = this.config.metrics
      .filter((m) => this.metricSeries.has(m.name))
      .slice(0, 5);

    if (metricsToDisplay.length === 0) {
      content = 'Waiting for metrics...';
    } else {
      metricsToDisplay.forEach(({ name }) => {
        const series = this.metricSeries.get(name)!;
        content += `{bold}${name}{/bold}  (last ${range}s)\n`;

        const all = series.flatMap((s) => s.values).filter((v): v is number => v !== null);
        if (all.length === 0) {
          content += 'No data\n\n';
          return;
        }

        let min = Math.min(...all);
        let max = Math.max(...all);
        // Center a flat line instead of dividing by zero
        const pad = max === min ? Math.abs(max) * 0.1 || 1 : 0;
        min -= pad;
        max += pad;

        const cells = plotBraille(series.map((s) => s.values), GRAPH_COLS, GRAPH_ROWS, min, max);
        cells.forEach((row, r) => {
          const label =
            r === 0 ? this.formatValue(name, max) : r === GRAPH_ROWS - 1 ? this.formatValue(name, min) : '';
          content += `${label.padStart(Y_LABEL_WIDTH)} ${label ? '┤' : '│'}`;
          content += row
            .map((c) => {
              if (c.series < 0) return c.char;
              const color = SERIES_COLORS[c.series % SERIES_COLORS.length];
              return `{${color}-fg}${c.char}{/${color}-fg}`;
            })
            .join('');
          content += '\n';
        });
        content += ' '.repeat(Y_LABEL_WIDTH + 1) + '└' + '─'.repeat(GRAPH_COLS) + '\n';

        // Legend: one entry per series with its latest value, as many as fit on one line
        let legend = '';
        let used = 0;
        series.forEach((s, i) => {
          const latest = s.values.filter((v) => v !== null).at(-1);
          const text = `● ${s.label.slice(0, 40)} ${latest != null ? this.formatValue(name, latest) : 'N/A'}`;
          if (used + text.length > 78) return;
          const color = SERIES_COLORS[i % SERIES_COLORS.length];
          legend += `{${color}-fg}${blessed.escape(text)}{/${color}-fg}  `;
          used += text.length + 2;
        });
        content += legend + '\n';
      });
    }

    grid.setContent(content);
  }

  private updateScore(score: blessed.Widgets.BoxElement) {
    const totalDataPoints = this.countDataPoints();
    const metricCount = this.metricSeries.size;

    score.setContent(`${totalDataPoints} DR ${metricCount}`);
  }

  private updateStats(stats: blessed.Widgets.BoxElement) {
    const totalDataPoints = this.countDataPoints();
    const avgPerMetric = this.metricSeries.size > 0
      ? Math.floor(totalDataPoints / this.metricSeries.size)
      : 0;

    stats.setContent(`${totalDataPoints} | ${avgPerMetric}`);
  }

  private countDataPoints(): number {
    let count = 0;
    this.metricSeries.forEach((series) =>
      series.forEach((s) => s.values.forEach((v) => v !== null && count++))
    );
    return count;
  }

  // Fetches the whole display window via query_range, one sample per dot column.
  private async fetchRange(query: string): Promise<Series[] | null> {
    try {
      const range = this.config.range ?? 300;
      const step = range / (GRAPH_POINTS - 1);
      // Align to step so samples don't shift between refreshes
      const end = Math.floor(Date.now() / 1000 / step) * step;
      const start = end - step * (GRAPH_POINTS - 1);
      const params = new URLSearchParams({
        query,
        start: String(start),
        end: String(end),
        step: String(step),
      });
      const url = `${this.config.prometheus.protocol}://${this.config.prometheus.host}:${this.config.prometheus.port}/api/v1/query_range?${params}`;

      const response = await fetch(url);
      const data = (await response.json()) as {
        status: string;
        data: { result: Array<{ metric: Record<string, string>; values: [number, string][] }> };
      };

      if (data.status !== 'success') return null;
      return data.data.result.map((r) => {
        const values = new Array<number | null>(GRAPH_POINTS).fill(null);
        for (const [ts, v] of r.values) {
          const x = Math.round((ts - start) / step);
          const n = parseFloat(v);
          if (x >= 0 && x < GRAPH_POINTS && isFinite(n)) values[x] = n;
        }
        const label = Object.entries(r.metric)
          .filter(([k]) => k !== '__name__')
          .map(([k, v]) => `${k}=${v}`)
          .join(',');
        return { label: label || 'value', values };
      });
    } catch (error) {
      return null;
    }
  }

  private async startMetricsCollection() {
    const collectMetrics = async () => {
      await Promise.all(
        this.config.metrics.map(async (metric) => {
          const series = await this.fetchRange(metric.query);
          // Keep the last good data on failure
          if (series !== null) this.metricSeries.set(metric.name, series);
        })
      );
    };

    // Initial collection
    await collectMetrics();

    // Regular updates
    setInterval(collectMetrics, this.config.refresh_interval);
  }

  public render() {
    this.screen.render();
  }
}

// Start the dashboard
const dashboard = new PrometheusDash();
dashboard.render();
