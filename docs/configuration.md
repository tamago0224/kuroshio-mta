# 設定

`kuroshio-mta` の設定は、YAML と環境変数の組み合わせで管理できます。README に載せていた設定詳細は、このページでまとめて管理します。

## 優先順位

1. デフォルト値
2. `MTA_CONFIG_FILE` で指定した YAML、またはカレントディレクトリの `config.yaml` / `config.yml`
3. 環境変数

同じ設定を複数の方法で指定した場合は、最後に評価される環境変数が優先されます。`MTA_CONFIG_FILE` を指定した場合は、そのパスにファイルが存在しないと起動時にエラーになります。

## YAML設定

`MTA_CONFIG_FILE` に YAML ファイルを指定すると、`gopkg.in/yaml.v3` で設定を読み込みます。未指定でも作業ディレクトリ直下の `config.yaml` または `config.yml` があれば自動で読み込みます。

```bash
MTA_CONFIG_FILE=./config.yaml go run ./cmd/kuroshio
```

サンプルは [config.example.yaml](/home/tamago/ghq/github.com/tamago/kuroshio-mta/config.example.yaml) にあります。

補足:
- YAML では `internal/config.Config` に対応する値をまとめて定義できます
- `MTA_SUBMISSION_USERS_FILE` と `MTA_ADMIN_TOKENS_FILE` はこれまで通りシークレットファイル用途で使えます
- secret 値を YAML に直書きしたくない場合は、YAML で機能を有効にしつつ秘密情報だけ環境変数や `*_FILE` に逃がす運用ができます

## 主要環境変数

- `MTA_LISTEN_ADDR` (default: `:2525`)
- `MTA_SUBMISSION_ADDR` (default: unset, set e.g. `:587` to enable Submission listener)
- `MTA_SUBMISSION_AUTH_REQUIRED` (default: `true`)
- `MTA_SUBMISSION_USERS` (default: unset, format: `user@example.com:password,...`)
- `MTA_SUBMISSION_USERS_FILE` (default: unset, `MTA_SUBMISSION_USERS` の代替。ファイルからシークレット読込)
- `MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY` (default: `true`, requires `MAIL FROM` domain to match authenticated user domain)
- `MTA_LOG_LEVEL` (default: `info`, values: `debug` / `info` / `warn` / `error`, logs are JSON via `slog`)
- `MTA_OBSERVABILITY_ADDR` (default: `:9090`)
- `MTA_ADMIN_ADDR` (default: unset)
- `MTA_ADMIN_TOKENS` (default: unset, format: `viewer-token:viewer,operator-token:operator`)
- `MTA_REPUTATION_START_DATE` (default: unset, format: `YYYY-MM-DD`)
- `MTA_REPUTATION_WARMUP_RULES` (default: `0:100,7:1000,14:5000`)
- `MTA_REPUTATION_BOUNCE_THRESHOLD` (default: `0.05`)
- `MTA_REPUTATION_COMPLAINT_THRESHOLD` (default: `0.001`)
- `MTA_REPUTATION_MIN_SAMPLES` (default: `100`)
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
- `MTA_KAFKA_CONSUMER_GROUP` (default: `kuroshio-mta`)
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
  ARC署名も同じ鍵設定を利用し、鍵ファイル更新時は送信時に自動リロードします
- `MTA_ARC_FAILURE_POLICY` (default: `accept`, values: `accept` / `quarantine` / `reject`)
- `MTA_SPF_HELO_POLICY` (default: `advisory`, values: `off` / `advisory` / `enforce`)
- `MTA_SPF_MAILFROM_POLICY` (default: `advisory`, values: `off` / `advisory` / `enforce`)

## Rate Limit Rules

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

## Kafka Queue Mode

```bash
MTA_QUEUE_BACKEND="kafka"
MTA_KAFKA_BROKERS="localhost:9092"
MTA_KAFKA_CONSUMER_GROUP="kuroshio-mta"
MTA_KAFKA_TOPIC_INBOUND="mail.inbound"
MTA_KAFKA_TOPIC_RETRY="mail.retry"
MTA_KAFKA_TOPIC_DLQ="mail.dlq"
MTA_KAFKA_TOPIC_SENT="mail.sent"
```

Kafka のローカル起動例:

```bash
docker compose -f docker-compose.kafka.yml up -d
```

## 関連ドキュメント

- 運用 API: [admin_api.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/runbooks/admin_api.md)
- SLO runbook: [slo_backlog.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/runbooks/slo_backlog.md), [slo_delivery.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/runbooks/slo_delivery.md), [slo_retry.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/runbooks/slo_retry.md)
- シークレット運用: [secrets_and_supply_chain.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/security/secrets_and_supply_chain.md)
