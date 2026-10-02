# Prometheus Dashboard TUI

A TUI (Terminal User Interface) dashboard for Prometheus metrics visualization.

![Prometheus Dashboard](pix/prom-dash.png)

## Features

- Real-time metrics visualization from Prometheus
- Pixel-level graphs on terminals with image support (kitty graphics protocol / iTerm2 inline images), braille line graphs (2x4 dots per cell) elsewhere
- Color themes: retro, tokyonight, kanagawa-wave, solarized, dracula, gruvbox
- History fetched via `query_range`, so graphs are filled immediately on startup
- Multiple series per query, drawn in different colors with a legend
- Dynamic vertical scaling for better visualization
- Configurable refresh intervals (default: 500ms)

## Prerequisites

- [Go](https://go.dev/) 1.27+
- Docker and Docker Compose (for running Prometheus and Node Exporter)

## Quick Start

### 1. Start Prometheus and Node Exporter

```bash
cd example
docker compose up -d
cd ..
```

This will start:
- Prometheus server on `http://localhost:9090`
- Node Exporter on `http://localhost:9100`

### 2. Run the dashboard

```bash
go run .
```

or build a binary:

```bash
go build -o prom-dash .
./prom-dash
```

The dashboard reads `prom-dash.yaml` from the current directory.

## Configuration

Edit `prom-dash.yaml` to customize metrics and connection settings:

```yaml
prometheus:
  host: localhost
  port: 9090
  protocol: http

refresh_interval: 500  # milliseconds
range: 300  # seconds of history to display (default: 300)
theme: retro  # retro (default), tokyonight, kanagawa-wave, solarized, dracula, gruvbox
graphics: auto  # auto (default), kitty, iterm2, braille

metrics:
  - name: cpu_usage
    query: 'sum(rate(node_cpu_seconds_total{mode!="idle"}[10s]))'
  - name: net_receive_bytes
    query: 'rate(node_network_receive_bytes_total{device="eth0"}[10s])'
  - name: net_transmit_bytes
    query: 'rate(node_network_transmit_bytes_total{device="eth0"}[10s])'
  - name: net_receive_packets
    query: 'rate(node_network_receive_packets_total{device="eth0"}[10s])'
  - name: net_transmit_packets
    query: 'rate(node_network_transmit_packets_total{device="eth0"}[10s])'
```

### Graphics

`graphics: auto` picks the renderer from the environment:

| Terminal | Renderer |
|---|---|
| kitty, Ghostty, WezTerm (also inside herdr) | kitty graphics protocol |
| iTerm2 | iTerm2 inline images |
| inside tmux / zellij, others | braille |

Set `kitty`, `iterm2` or `braille` explicitly to override.

## Usage

### Controls

- `q` or `ESC` or `Ctrl+C`: Exit the dashboard

## License

MIT License

## Author

jedipunkz

