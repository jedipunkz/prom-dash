import blessed from 'blessed';
import * as fs from 'fs';
import * as yaml from 'js-yaml';
import * as path from 'path';

interface Config {
  prometheus: {
    host: string;
    port: number;
    protocol: string;
  };
  refresh_interval: number;
  metrics: Array<{
    name: string;
    query: string;
  }>;
}

interface MetricData {
  name: string;
  value: number;
  timestamp: number;
}

interface MetricTimeSeries {
  name: string;
  values: number[];
  maxValue: number;
}

class PrometheusDash {
  private screen: blessed.Widgets.Screen;
  private config: Config;
  private metrics: MetricData[] = [];
  private startTime: number;
  private metricTimeSeries: Map<string, number[]> = new Map();

  constructor() {
    // Load config
    const configPath = path.join(process.cwd(), 'prometheus-dash.yaml');
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
    const graphWidth = 60;
    const graphHeight = 6;
    let content = '';

    const metricsToDisplay = Array.from(this.metricTimeSeries.entries()).slice(0, 5);

    if (metricsToDisplay.length === 0) {
      content = 'Waiting for metrics...';
    } else {
      metricsToDisplay.forEach(([name, values]) => {
        // Display metric name
        content += `{bold}${name}{/bold}\n`;

        // Draw graph
        if (values.length === 0) {
          content += 'No data\n\n';
          return;
        }

        const displayValues = values.slice(-graphWidth);

        // Calculate dynamic range based on displayed values
        const maxValue = Math.max(...displayValues);
        const minValue = Math.min(...displayValues);
        const range = maxValue - minValue;

        // Use a minimum range to avoid division by zero and show some variation
        const effectiveRange = range < 0.0001 ? 0.0001 : range;
        const effectiveMin = range < 0.0001 ? minValue - 0.00005 : minValue;

        // Draw from top to bottom (high to low)
        for (let row = graphHeight - 1; row >= 0; row--) {
          const threshold = effectiveMin + (row / (graphHeight - 1)) * effectiveRange;
          let line = '';

          for (let col = 0; col < graphWidth; col++) {
            if (col < displayValues.length) {
              const value = displayValues[col];
              const normalizedValue = (value - effectiveMin) / effectiveRange;

              if (value >= threshold) {
                // Use different characters based on density
                if (normalizedValue > 0.7) {
                  line += '█';
                } else if (normalizedValue > 0.4) {
                  line += '▓';
                } else if (normalizedValue > 0.2) {
                  line += '▒';
                } else {
                  line += '░';
                }
              } else {
                line += ' ';
              }
            } else {
              line += ' ';
            }
          }

          content += line + '\n';
        }

        // Add axis and spacing
        content += '─'.repeat(graphWidth) + '\n';
        const latestValue = displayValues[displayValues.length - 1];
        content += `Range: ${this.formatValue(name, minValue)}-${this.formatValue(name, maxValue)}  Latest: ${latestValue !== undefined ? this.formatValue(name, latestValue) : 'N/A'}\n`;
      });
    }

    grid.setContent(content);
  }

  private updateScore(score: blessed.Widgets.BoxElement) {
    const totalDataPoints = Array.from(this.metricTimeSeries.values()).reduce(
      (sum, values) => sum + values.length,
      0
    );
    const metricCount = this.metricTimeSeries.size;

    score.setContent(`${totalDataPoints} DR ${metricCount}`);
  }

  private updateStats(stats: blessed.Widgets.BoxElement) {
    const totalDataPoints = Array.from(this.metricTimeSeries.values()).reduce(
      (sum, values) => sum + values.length,
      0
    );
    const avgPerMetric = this.metricTimeSeries.size > 0
      ? Math.floor(totalDataPoints / this.metricTimeSeries.size)
      : 0;

    stats.setContent(`${totalDataPoints} | ${avgPerMetric}`);
  }

  private async fetchMetric(query: string): Promise<number | null> {
    try {
      const url = `${this.config.prometheus.protocol}://${this.config.prometheus.host}:${this.config.prometheus.port}/api/v1/query?query=${encodeURIComponent(query)}`;

      const response = await fetch(url);
      const data = await response.json();

      if (data.status === 'success' && data.data.result.length > 0) {
        const value = parseFloat(data.data.result[0].value[1]);
        return isNaN(value) ? null : value;
      }
      return null;
    } catch (error) {
      return null;
    }
  }

  private async startMetricsCollection() {
    const collectMetrics = async () => {
      const promises = this.config.metrics.map(async (metric) => {
        const value = await this.fetchMetric(metric.query);
        if (value !== null) {
          return {
            name: metric.name,
            value,
            timestamp: Date.now(),
          };
        }
        return null;
      });

      const results = await Promise.all(promises);
      this.metrics = results.filter((m): m is MetricData => m !== null);

      // Update time series data for each metric
      this.metrics.forEach((m) => {
        if (!this.metricTimeSeries.has(m.name)) {
          this.metricTimeSeries.set(m.name, []);
        }

        const series = this.metricTimeSeries.get(m.name)!;
        series.push(m.value);

        // Keep only last 100 data points per metric
        if (series.length > 100) {
          this.metricTimeSeries.set(m.name, series.slice(-100));
        }
      });
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
