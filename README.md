# kuroshio-mta

<p align="center">
  <img src="./docs/public/kuroshio-logo.png" alt="kuroshio-mta logo" width="180">
</p>

`kuroshio-mta` は Go で実装した MTA（Mail Transfer Agent）です。

黒潮は日本近海を流れる世界有数の海流で、
大量のメールを力強く安定して運ぶ MTA をイメージして命名しています。

## 特徴

- Go で実装した、外部 SMTP ライブラリ非依存の MTA
- SMTP 受信サーバーと Submission サーバーを備え、`SMTP AUTH PLAIN / LOGIN` に対応
- ローカル永続キューと配送ワーカーを持ち、MX 解決ベースの配送と再送バックオフを実装
- DKIM / ARC 送信署名と、SPF / DKIM / DMARC / ARC の受信評価に対応
- STARTTLS、MTA-STS、DANE など送受信時のセキュリティ機能を段階的に実装
- Kafka queue mode、Redis / Valkey ベースの rate limit、S3-compatible spool backend を選択可能

## ドキュメント

詳細なドキュメントは GitHub Pages で公開しています。

- docs サイト: https://tamago0224.github.io/kuroshio-mta/
- Getting Started: https://tamago0224.github.io/kuroshio-mta/getting-started
- Configuration: https://tamago0224.github.io/kuroshio-mta/configuration
- Tutorials: https://tamago0224.github.io/kuroshio-mta/tutorials/basic-mail-flow

リポジトリ内の Markdown を直接参照したい場合は、以下を入口にしてください。

- [docs/index.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/index.md)
- [Getting Started](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/getting-started.md)
- [Configuration](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/configuration.md)

## クイックスタート

```bash
cp config.example.yaml config.yaml
go run ./cmd/kuroshio -config ./config.yaml
```

`-config` を省略した場合は、`MTA_CONFIG_FILE` またはカレントディレクトリの `config.yaml` / `config.yml` を順に参照します。

## Docker

本体用の [Dockerfile](/home/tamago/ghq/github.com/tamago/kuroshio-mta/Dockerfile) を用意しています。

```bash
docker build -t kuroshio-mta:latest .
docker run --rm \
  -p 2525:2525 \
  -p 9090:9090 \
  -v "$(pwd)/config.yaml:/etc/kuroshio/config.yaml:ro" \
  kuroshio-mta:latest
```

補足:
- デフォルトでは `-config /etc/kuroshio/config.yaml` を使います
- TLS 証明書、鍵、queue ディレクトリ、spool ディレクトリを使う場合は必要なパスを追加で mount してください
- `submission_addr` や `admin_addr` を有効にする場合は、必要なポートも `-p` で公開してください

## 開発と確認

- docs サイト:
  `npm install`
  `npm run docs:dev`
- docs build:
  `npm run docs:build`
- SMTP conformance:
  `go test ./internal/smtp -run '^TestSMTPConformance$' -v`
- DNS 結合テスト:
  `./scripts/integration/run_dns_env_tests.sh`

## リポジトリ内の補助ファイル

- 設定サンプル: [config.example.yaml](/home/tamago/ghq/github.com/tamago/kuroshio-mta/config.example.yaml)
- Prometheus alert rule ひな形: [kuroshio_slo_rules.yml](/home/tamago/ghq/github.com/tamago/kuroshio-mta/deploy/monitoring/prometheus/kuroshio_slo_rules.yml)
- Docker 設定: [Dockerfile](/home/tamago/ghq/github.com/tamago/kuroshio-mta/Dockerfile), [.dockerignore](/home/tamago/ghq/github.com/tamago/kuroshio-mta/.dockerignore)
