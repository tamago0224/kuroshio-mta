# Getting Started

`kuroshio-mta` をローカルで立ち上げて、次にどのドキュメントを読めばよいかをまとめた入口ページです。

## 1. 前提

- Go `1.25.x`
- リポジトリを clone 済み
- ローカルで `:2525` や `:9090` を使えること

VitePress の docs サイトをローカル確認したい場合だけ、追加で Node.js が必要です。

## 2. サンプル設定を用意する

まずは [config.example.yaml](https://github.com/tamago0224/kuroshio-mta/blob/main/config.example.yaml) を元に
`config.yaml` を作るのがいちばん簡単です。

最低限見ることが多い項目:

- `listen_addr`
- `hostname`
- `queue_dir`
- `queue_backend`
- `delivery_mode`
- `spool_backend`

設定項目の全体像は [Configuration](/configuration) にまとまっています。

## 3. MTA を起動する

```bash
go run ./cmd/kuroshio -config ./config.yaml
```

`-config` を省略した場合は、`MTA_CONFIG_FILE` またはカレントディレクトリの
`config.yaml` / `config.yml` が使われます。

## 4. 起動後に見る場所

- SMTP listener: `:2525`
- Observability: `:9090`
- ローカルキュー: `./var/queue`

Admin API を有効化する場合は [Admin API](/runbooks/admin_api)、
SLO を見たい場合は [SLO Delivery](/runbooks/slo_delivery) を参照してください。

## 5. 次に試すチュートリアル

- 最初の 1 通を受ける: [最小メールフローを試す](/tutorials/basic-mail-flow)
- 認証評価を見る: [メール認証を試す](/tutorials/mail-auth)
- STARTTLS / MTA-STS / DANE を追う: [TLS 配送ポリシーを試す](/tutorials/tls-policy)
- 受信制御を試す: [Rate Limit を試す](/tutorials/rate-limit)
- queue 操作を試す: [Admin API を試す](/tutorials/admin-operations)

## 6. よく読む関連ドキュメント

- 初期設定: [Configuration](/configuration)
- 受信制御: [Rate Limit](/rate_limit)
- Kafka バックエンド: [Kafka Queue Mode](/kafka_queue_mode)
- 設計メモ: [Normalization Policy](/architecture/normalization_policy)
- HA 構成: [HA Reference](/architecture/ha_reference)

## 7. docs サイトをローカル確認する

```bash
npm install
npm run docs:dev
```

静的 build だけ確認したい場合は次を使います。

```bash
npm run docs:build
```
