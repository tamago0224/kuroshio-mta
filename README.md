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

## RFC対応状況（現時点）

| RFC | 技術 | 対応状況 | 補足 |
| --- | --- | --- | --- |
| RFC 5321 | SMTP | 一部対応 | `EHLO/HELO`, `MAIL FROM`, `RCPT TO`, `DATA`, `RSET`, `NOOP`, `QUIT` を実装 |
| RFC 3207 | SMTP STARTTLS | 対応済み（実装範囲内） | 受信側/送信側で STARTTLS 昇格を実装 |
| RFC 6531 | SMTPUTF8 | 非対応（方針確定） | `SMTPUTF8` パラメータと UTF-8 メールアドレスは明示的に拒否（`555`/`553`） |
| RFC 7208 | SPF | 一部対応 | `ip4`, `ip6`, `a`, `mx`, `include`, `all` を評価 |
| RFC 6376 | DKIM | 一部対応 | DKIM署名検証（`rsa-sha256`）を実装 |
| RFC 7489 | DMARC | 一部対応 | SPF/DKIM alignment と `p` ポリシー評価を実装 |
| RFC 8617 | ARC | 一部対応 | ARCセットの構造検証（チェーン整合）を実装 |
| RFC 8461 | MTA-STS | 一部対応 | policy取得・キャッシュ・enforce適用を実装 |
| RFC 7672 | DANE for SMTP | 一部対応 | TLSA取得と優先適用（DANE > MTA-STS）を実装 |
| RFC 3464 | DSN | 一部対応 | DSNパースと suppression 連携を実装 |

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
- `MTA_QUEUE_BACKEND` (default: `local`, values: `local` / `kafka`)
- `MTA_KAFKA_BROKERS` (default: `localhost:9092`, comma-separated)
- `MTA_KAFKA_CONSUMER_GROUP` (default: `orinoco-mta`)
- `MTA_KAFKA_TOPIC_INBOUND` (default: `mail.inbound`)
- `MTA_KAFKA_TOPIC_RETRY` (default: `mail.retry`)
- `MTA_KAFKA_TOPIC_DLQ` (default: `mail.dlq`)
- `MTA_KAFKA_TOPIC_SENT` (default: `mail.sent`)
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

## Rate Limit Rules Examples

`MTA_RATE_LIMIT_RULES` は `event:key:limit:window;...` 形式で指定します。

- `event`: `connect` / `helo` / `mailfrom`
- `key`: `ip` / `helo` / `mailfrom` / `ip+helo` / `ip+mailfrom`
- `limit`: 許可回数
- `window`: 期間（例: `10s`, `1m`, `5m`, `1h`）

例:

```bash
# 1分間に接続100回まで（IP単位）
MTA_RATE_LIMIT_RULES="connect:ip:100:1m"

# 1分間に HELO ごとに 20回まで（IP+HELO 単位）
MTA_RATE_LIMIT_RULES="helo:ip+helo:20:1m"

# 5分間に MAIL FROM ごとに 30回まで（IP+MAIL FROM 単位）
MTA_RATE_LIMIT_RULES="mailfrom:ip+mailfrom:30:5m"

# 複数ルールの組み合わせ
MTA_RATE_LIMIT_RULES="connect:ip:100:1m;helo:ip+helo:20:1m;mailfrom:ip+mailfrom:30:5m"
```

## 開発方針 (TDD)

今後の機能追加・修正は、以下の順で進めます。

1. 先に失敗するテストを書く (`Red`)
2. 最小実装でテストを通す (`Green`)
3. 振る舞いを維持したまま整理する (`Refactor`)

## Kafka Queue Mode Example

```bash
MTA_QUEUE_BACKEND="kafka"
MTA_KAFKA_BROKERS="localhost:9092"
MTA_KAFKA_CONSUMER_GROUP="orinoco-mta"
MTA_KAFKA_TOPIC_INBOUND="mail.inbound"
MTA_KAFKA_TOPIC_RETRY="mail.retry"
MTA_KAFKA_TOPIC_DLQ="mail.dlq"
MTA_KAFKA_TOPIC_SENT="mail.sent"
```

Kafka のローカル起動例:

```bash
docker compose -f docker-compose.kafka.yml up -d
```

## DNS結合テスト環境

受信側の `mailauth`（SPF/DMARC）と送信側の DANE/MTA-STS の検証用に、
DNS を含む `docker compose` 環境を用意しています。

```bash
./scripts/integration/run_dns_env_tests.sh
```

詳細は `test/integration/README.md` を参照してください。
