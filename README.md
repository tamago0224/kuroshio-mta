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
- `MTA_OBSERVABILITY_ADDR` (default: `:9090`)
- `MTA_HOSTNAME` (default: `orinoco.local`)
- `MTA_QUEUE_DIR` (default: `./var/queue`)
- `MTA_TLS_CERT_FILE` (default: unset)
- `MTA_TLS_KEY_FILE` (default: unset)
- `MTA_INGRESS_RATE_LIMIT_PER_MINUTE` (default: `100`)
- `MTA_RATE_LIMIT_RULES` (default: unset, format: `event:key:limit:window;...`)
- `MTA_DNSBL_ZONES` (default: unset, comma-separated)
- `MTA_DNSBL_CACHE_TTL` (default: `5m`)
- `MTA_MTA_STS_CACHE_TTL` (default: `1h`)
- `MTA_MTA_STS_FETCH_TIMEOUT` (default: `5s`)
- `MTA_DELIVERY_MODE` (default: `mx`, values: `mx` / `local_spool` / `relay`)
- `MTA_LOCAL_SPOOL_DIR` (default: `./var/spool`)
- `MTA_RELAY_HOST` (default: unset)
- `MTA_RELAY_PORT` (default: `25`)
- `MTA_RELAY_REQUIRE_TLS` (default: `false`)
- `MTA_MAX_MESSAGE_BYTES` (default: `10485760`)
- `MTA_WORKER_COUNT` (default: `4`)
- `MTA_MAX_ATTEMPTS` (default: `12`)
- `MTA_MAX_RETRY_AGE` (default: `120h`)
- `MTA_RETRY_SCHEDULE` (default: `5m,30m,2h,6h,24h`)
- `MTA_SCAN_INTERVAL` (default: `5s`)
- `MTA_DIAL_TIMEOUT` (default: `8s`)
- `MTA_SEND_TIMEOUT` (default: `20s`)

## 補足

このリポジトリのコアは、SMTPプロトコル処理を外部SMTPライブラリに依存せず実装しています。

## 開発方針 (TDD)

今後の機能追加・修正は、以下の順で進めます。

1. 先に失敗するテストを書く (`Red`)
2. 最小実装でテストを通す (`Green`)
3. 振る舞いを維持したまま整理する (`Refactor`)
