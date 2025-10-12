# Prometheus Dashboard

レトロスタイルのTUIベースPrometheusメトリクスダッシュボード

## 機能

- Prometheusサーバーからリアルタイムでメトリクスを取得
- レトロゲーム風のビジュアライゼーション
- チェッカーボードパターンでデータを表示
- タイマー、スコア、統計情報の表示

## セットアップ

1. 依存関係のインストール:
```bash
bun install
```

2. Prometheusサーバーの起動（Dockerを使用）:
```bash
cd example
docker compose up -d
cd ..
```

3. ダッシュボードの起動:
```bash
bun start
```

## 設定

`prometheus-dash.yaml` でPrometheusサーバーの設定とメトリクスクエリをカスタマイズできます:

```yaml
prometheus:
  host: localhost
  port: 9090
  protocol: http

refresh_interval: 1000  # ミリ秒

metrics:
  - name: cpu_usage
    query: 'rate(node_cpu_seconds_total{mode="user"}[1m])'
  # ... 他のメトリクス
```

## 操作方法

- `q` または `ESC` または `Ctrl+C`: 終了

## プロジェクト構成

```
.
├── prometheus-dash.yaml  # 設定ファイル
├── package.json
├── tsconfig.json
├── src/
│   └── index.ts          # メインアプリケーション
└── example/              # Docker Compose設定
    ├── docker-compose.yml
    └── prometheus.yml
```
