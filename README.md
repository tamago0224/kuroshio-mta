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
- `MTA_SUBMISSION_ADDR` (default: unset, set e.g. `:587` to enable Submission listener)
- `MTA_SUBMISSION_AUTH_REQUIRED` (default: `true`)
- `MTA_SUBMISSION_USERS` (default: unset, format: `user@example.com:password,...`)
- `MTA_SUBMISSION_USERS_FILE` (default: unset, `MTA_SUBMISSION_USERS` の代替。ファイルからシークレット読込)
- `MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY` (default: `true`, requires `MAIL FROM` domain to match authenticated user domain)
- `MTA_LOG_LEVEL` (default: `info`, values: `debug` / `info` / `warn` / `error`, logs are JSON via `slog`)
- `MTA_OBSERVABILITY_ADDR` (default: `:9090`)
- `MTA_SLO_MIN_DELIVERY_SUCCESS_RATE` (default: `0.99`)
- `MTA_SLO_MAX_RETRY_RATE` (default: `0.20`)
- `MTA_SLO_MAX_QUEUE_BACKLOG` (default: `50000`)
- `MTA_DATA_RETENTION_SENT` (default: `720h` = 30d)
- `MTA_DATA_RETENTION_DLQ` (default: `2160h` = 90d)
- `MTA_DATA_RETENTION_POISON` (default: `4320h` = 180d)
- `MTA_RETENTION_SWEEP_INTERVAL` (default: `1h`)
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
- `MTA_DANE_DNSSEC_TRUST_MODEL` (default: `ad_required`, values: `ad_required` / `insecure_allow_unsigned`)
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
- `MTA_DOMAIN_MAX_CONCURRENT_DEFAULT` (default: `8`)
- `MTA_DOMAIN_MAX_CONCURRENT_RULES` (default: unset, format: `gmail.com:2,yahoo.com:1`)
- `MTA_DOMAIN_ADAPTIVE_THROTTLE` (default: `true`)
- `MTA_DOMAIN_TEMPFAIL_THRESHOLD` (default: `0.3`)
- `MTA_DOMAIN_PENALTY_MAX` (default: `5s`)
- `MTA_SCAN_INTERVAL` (default: `5s`)
- `MTA_DIAL_TIMEOUT` (default: `8s`)
- `MTA_SEND_TIMEOUT` (default: `20s`)
- `MTA_DKIM_SIGN_DOMAIN` (default: unset)
- `MTA_DKIM_SIGN_SELECTOR` (default: unset)
- `MTA_DKIM_PRIVATE_KEY_FILE` (default: unset, PEM RSA private key)
- `MTA_DKIM_SIGN_HEADERS` (default: `from:to:subject:date:message-id`)

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

## SMTP Conformance Test

SMTP RFC の主要要件を `internal/smtp` のコンフォーマンステストで確認できます。

```bash
go test ./internal/smtp -run '^TestSMTPConformance$' -v
```

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

## SLO/SLI Monitoring

- `/metrics`: Prometheus metrics
- `/slo`: 現在の SLI/SLO 判定結果（JSON, breach時は HTTP 503）

Prometheus alert rule の雛形は [orinoco_slo_rules.yml](/home/tamago/ghq/github.com/tamago/orinoco-mta/deploy/monitoring/prometheus/orinoco_slo_rules.yml) に配置しています。

## HA Reference

- リファレンス構成とフェイルオーバー手順:
  [ha_reference.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/architecture/ha_reference.md)
- 障害注入ドリル補助スクリプト:
  `scripts/chaos/run_ha_drill.sh`

## Compliance Basics

- ログは `slog(JSON)` で出力
- メールアドレス等のPIIはログ出力時にマスキング
- `sent / mail.dlq / mail.dlq/poison` は保持期間ポリシーに基づき自動削除

## Security

- 脆弱性スキャン: `.github/workflows/security.yml` で `govulncheck` を実行
- SBOM生成: `scripts/security/generate_sbom.sh`
- 詳細方針: [secrets_and_supply_chain.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/security/secrets_and_supply_chain.md)
