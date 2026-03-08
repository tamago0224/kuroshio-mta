# orinoco-mta

`orinoco-mta` は Go で実装した MTA（Mail Transfer Agent）です。

Orinoco は南米を流れる川の名前で、
大量のメールをそつなく安定して流すことを期待した命名です。

## 現在の実装範囲

- SMTP受信サーバー（自前パーサー）
- ローカル永続キュー（`var/queue`）
- MX解決による配送先ルーティング
- SMTP配送ワーカー（再送バックオフ付き）
- STARTTLS対応先への送信時TLS昇格

## 実行方法

```bash
go run ./cmd/mta
```

デフォルトでは `:2525` でSMTP待受します。

## 主要環境変数

- `MTA_LISTEN_ADDR` (default: `:2525`)
- `MTA_HOSTNAME` (default: `orinoco.local`)
- `MTA_QUEUE_DIR` (default: `./var/queue`)
- `MTA_MAX_MESSAGE_BYTES` (default: `10485760`)
- `MTA_WORKER_COUNT` (default: `4`)
- `MTA_SCAN_INTERVAL` (default: `5s`)
- `MTA_DIAL_TIMEOUT` (default: `8s`)
- `MTA_SEND_TIMEOUT` (default: `20s`)

## 補足

このリポジトリのコアは、SMTPプロトコル処理を外部SMTPライブラリに依存せず実装しています。
