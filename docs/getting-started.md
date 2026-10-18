# Getting Started

`kuroshio-mta` をローカルで立ち上げて、次にどのドキュメントを読めばよいかをまとめた入口ページです。

## 1. 前提

- Go `1.26.9` 以降
- リポジトリを clone 済み
- ローカルで `:2525` や `:9090` を使えること

VitePress の docs サイトをローカル確認したい場合だけ、追加で Node.js が必要です。

## 2. サンプル設定を用意する

ローカル確認用の `config.local.example.yaml` をコピーします。既存の設定がある場合は別名で保存し、起動時の `-config` も変更してください。

```bash
cp config.local.example.yaml config.yaml
```

この設定は loopback で待ち受け、受信メールを `./var/spool` に保存します。
外部 SMTP への配送、Submission、TLS、DKIM 署名は有効にしていません。
`inbound_policy` は `user@recipient.test` だけをローカル宛先として受理します。
別の宛先で確認する場合は `local_domains` と `local_recipients` を両方更新してください。

全項目の設定例は [config.example.yaml](https://github.com/tamago0224/kuroshio-mta/blob/main/config.example.yaml) にあります。
この例を使う場合は `tls_cert_file` / `tls_key_file` の証明書・鍵と、
`dkim_private_key_file` の署名鍵を用意してください。証明書・鍵はリポジトリに含まれません。
TLS を使わない場合は Submission を無効化して TLS の2項目を削除し、署名しない場合は `dkim_sign_domain` / `dkim_sign_selector` / `dkim_private_key_file` を削除します。

最低限見ることが多い項目:

- `listen_addr`
- `hostname`
- `queue_dir`
- `queue_backend`
- `delivery_mode`
- `spool_backend`

設定項目の全体像は [Configuration](/configuration) にまとまっています。

## 3. MTA を起動する

起動前に、副作用なしで設定を検証できます。詳細と検証範囲は
[起動前の設定検証](configuration.md#起動前の設定検証) を参照してください。

```bash
go run ./cmd/kuroshio -check-config -config ./config.yaml
```

```bash
go run ./cmd/kuroshio -config ./config.yaml
```

`-config` を省略した場合は、`MTA_CONFIG_FILE` またはカレントディレクトリの
`config.yaml` / `config.yml` が使われます。

## 4. 起動後に見る場所

- SMTP listener: `127.0.0.1:2525`
- Observability: `127.0.0.1:9090`
- ローカルキュー: `./var/queue`
- 配送後のメール: `./var/spool`

受信から配送までを確認するには、Python 3 がある環境で次を実行します。
テスト用アドレスには予約済みの `.test` ドメインを使います。実在ドメインを使うと、そのドメインの認証ポリシーで拒否される場合があります。

```bash
python3 - <<'PYTHON'
import smtplib

with smtplib.SMTP("127.0.0.1", 2525) as smtp:
    smtp.sendmail(
        "sender@sender.test",
        ["user@recipient.test"],
        "From: sender@sender.test\r\nTo: user@recipient.test\r\n"
        "Subject: local test\r\n\r\nhello kuroshio\r\n",
    )
PYTHON
curl --fail http://127.0.0.1:9090/healthz
```

配送ワーカーの処理後（既定のスキャン間隔は5秒）に `./var/spool` 配下へ `.eml` が保存されます。

Admin API を有効化する場合は [Admin API](/runbooks/admin_api)、
SLO を見たい場合は [SLO Delivery](/runbooks/slo_delivery) を参照してください。

## 5. 次に試すチュートリアル

compose ベースの tutorial 一覧は [Tutorials](/tutorials/) にまとめています。

- 最初の 1 通を受ける: [最小メールフローを試す](/tutorials/basic-mail-flow)
- trace を見る: [OTEL Tracing を試す](/tutorials/otel-tracing)
- S3-compatible backend を見る: [S3 Spool Backend を観測する](/tutorials/s3-spool-observability)
- Kafka backend を見る: [Kafka Queue Mode を観測する](/tutorials/kafka-queue-observability)
- 認証評価を見る: [メール認証を試す](/tutorials/mail-auth)
- STARTTLS / MTA-STS / DANE を追う: [TLS 配送ポリシーを試す](/tutorials/tls-policy)
- 受信制御を試す: [Rate Limit を試す](/tutorials/rate-limit)
- 配送側 throttle を見る: [Domain Throttle を観測する](/tutorials/domain-throttle-observability)
- queue 操作を試す: [Admin API を試す](/tutorials/admin-operations)

## 6. 次に読む Guide

- 設定全体: [Configuration](/configuration)
- 全体像: [Observability Stack](/observability_stack)
- signal の使い分け: [Observability Signals](/observability_signals)
- 観測系: [Observability](/observability)
- 受信制御: [Rate Limit](/rate_limit)
- S3-compatible 保存先: [S3 Spool Backend](/s3_spool_backend)
- Kafka バックエンド: [Kafka Queue Mode](/kafka_queue_mode)

## 7. 設計と運用のドキュメント

- 設計メモ: [Normalization Policy](/architecture/normalization_policy)
- HA 構成: [HA Reference](/architecture/ha_reference)

## 8. docs サイトをローカル確認する

```bash
npm install
npm run docs:dev
```

静的 build だけ確認したい場合は次を使います。

```bash
npm run docs:build
```
