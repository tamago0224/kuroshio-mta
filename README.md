# orinoco-mta

`orinoco-mta` は Go で実装した MTA（Mail Transfer Agent）です。

Orinoco は南米を流れる川の名前で、
大量のメールをそつなく安定して流すことを期待した命名です。

## 現在の実装範囲

- SMTP受信サーバー（自前パーサー）
- Submission サーバー（SMTP AUTH PLAIN / LOGIN）
- ローカル永続キュー（`var/queue`）
- MX解決による配送先ルーティング
- SMTP配送ワーカー（再送バックオフ付き）
- STARTTLS対応先への送信時TLS昇格
- DKIM / ARC 送信署名、SPF / DKIM / DMARC / ARC 受信評価

## RFC対応状況（現時点）

注記:
- `対応済み（実装範囲内）` は、現行の Orinoco MTA が対象としている機能範囲では実装済みであることを示します。
- 周辺RFCとの完全な相互運用や、未採用オプションまで含む全面実装を意味するものではありません。

| RFC | 技術 | 対応状況 | 補足 |
| --- | --- | --- | --- |
| RFC 5321 | SMTP | 対応済み（実装範囲内） | `EHLO/HELO`, `MAIL FROM`, `RCPT TO`, `DATA`, `RSET`, `NOOP`, `QUIT`, `HELP`, `VRFY` を実装。`EXPN` は `502` 応答、`Received:` トレースヘッダ、`postmaster` 宛特例、主要な構文/状態遷移/行長制限のコンフォーマンステストを整備。拡張は各RFCで管理 |
| RFC 3207 | SMTP STARTTLS | 対応済み（実装範囲内） | 受信側/送信側で STARTTLS 昇格を実装 |
| RFC 1870 | SMTP SIZE | 対応済み（実装範囲内） | `SIZE` パラメータと最大メッセージサイズ制限を実装 |
| RFC 6152 | 8BITMIME | 対応済み（実装範囲内） | `BODY=8BITMIME` と 8bit 本文受信を実装 |
| RFC 4954 | SMTP AUTH | 一部対応 | Submission 経路で `AUTH PLAIN` / `AUTH LOGIN` を実装 |
| RFC 6409 | Message Submission | 一部対応 | Submission リスナ、認証必須化、送信者ドメイン制約を実装 |
| RFC 6531 | SMTPUTF8 | 非対応（方針確定） | `SMTPUTF8` パラメータと UTF-8 メールアドレスは明示的に拒否（`555`/`553`） |
| RFC 7208 | SPF | 一部対応 | `ip4`, `ip6`, `a`, `mx`, `include`, `exists`, `ptr`, `redirect`, `exp`, macro 展開、lookup 制限、HELO/MAIL FROM ポリシー分離を実装 |
| RFC 6376 | DKIM | 一部対応 | 受信時の DKIM 検証、複数署名評価、`l/t/x/h`、canonicalization、送信時 DKIM 署名を実装 |
| RFC 7489 | DMARC | 一部対応 | SPF/DKIM alignment、`p/sp/pct/fo/rf/ri/rua/ruf`、サブドメインポリシー、集計/失敗レポート生成を実装 |
| RFC 8617 | ARC | 一部対応 | ARC chain の構造検証・暗号検証、`i=` 連番検証、送信/中継時の ARC 署名付与、失敗時ポリシーを実装 |
| RFC 8461 | MTA-STS | 一部対応 | TXT `id` 検証、policy 取得・キャッシュ、`text/plain` 検証、stale 利用、`mode=enforce/testing`、安全なロールオーバー、MX wildcard 制約を実装。詳細は [rfc_8461_gap.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/rfc_8461_gap.md) |
| RFC 7672 | DANE for SMTP | 一部対応 | TLSA取得、CNAME 展開、`DANE-TA(2)` の証明書名チェック、優先適用（DANE > MTA-STS）を実装。詳細は [rfc_7672_gap.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/rfc_7672_gap.md) |
| RFC 3464 | DSN | 対応済み（実装範囲内） | DSN パース、DSN 生成（hard/soft bounce）、loop 防止、`Reporting-MTA` / `Status` 検証、相互運用テストを実装。詳細は [rfc_3464_gap.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/rfc_3464_gap.md) |

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
  - ARC署名も同じ鍵設定を利用し、鍵ファイル更新時は送信時に自動リロード
- `MTA_ARC_FAILURE_POLICY` (default: `accept`, values: `accept` / `quarantine` / `reject`)

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
- DMARC集計メトリクス（受信時）:
  - `smtp_auth_dmarc_result_<result>_total`（例: `fail`, `pass`, `none`）
  - `smtp_auth_dmarc_policy_<policy>_total`（例: `reject`, `quarantine`, `none`）
  - `smtp_auth_action_<action>_total`（例: `accept`, `quarantine`, `reject`）

Prometheus alert rule の雛形は [orinoco_slo_rules.yml](/home/tamago/ghq/github.com/tamago/orinoco-mta/deploy/monitoring/prometheus/orinoco_slo_rules.yml) に配置しています。

## HA Reference

- リファレンス構成とフェイルオーバー手順:
  [ha_reference.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/architecture/ha_reference.md)
- 障害注入ドリル補助スクリプト:
  `scripts/chaos/run_ha_drill.sh`

## Load / Chaos Testing

- 負荷試験 runbook:
  [load_chaos.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/runbooks/load_chaos.md)
- 単体シナリオ実行:
  `scripts/load/run_smtp_load.sh normal 127.0.0.1:2525`
- カオス併用スイート:
  `scripts/chaos/run_load_chaos_suite.sh 127.0.0.1:2525 --apply ./var/load-chaos/results.ndjson`
- 容量計画表への整形:
  `scripts/load/plan_capacity.sh ./var/load-chaos/results.ndjson`

## Admin API

- runbook:
  [admin_api.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/runbooks/admin_api.md)
- 最小CLI:
  `scripts/admin/orinoco_admin.sh`
- 対応操作:
  `suppression` 一覧/追加/削除、`retry` / `dlq` 一覧、再投入
- 認可:
  Bearer token + role（`viewer` / `operator` / `admin`）

## Reputation Controls

- runbook:
  [reputation_ops.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/runbooks/reputation_ops.md)
- 可視化:
  `GET /reputation`
- 記録:
  `scripts/admin/orinoco_admin.sh record-complaint gmail.com`
  `scripts/admin/orinoco_admin.sh record-tlsrpt gmail.com false`

## DR Backup / Restore

- DR（Disaster Recovery, 災害対策）向け runbook:
  [dr_backup_restore.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/runbooks/dr_backup_restore.md)
- バックアップ:
  `scripts/dr/backup_queue.sh ./var/queue ./var/backups`
- リストア:
  `scripts/dr/restore_queue.sh ./var/backups/orinoco-queue-<timestamp>.tar.gz ./var/queue --force`
- DRドリル:
  `scripts/chaos/run_dr_drill.sh ./var/queue ./var/backups --apply`

## Compliance Basics

- ログは `slog(JSON)` で出力
- メールアドレス等のPIIはログ出力時にマスキング
- `sent / mail.dlq / mail.dlq/poison` は保持期間ポリシーに基づき自動削除

## Security

- 脆弱性スキャン: `.github/workflows/security.yml` で `govulncheck` を実行
- SBOM生成: `scripts/security/generate_sbom.sh`
- 詳細方針: [secrets_and_supply_chain.md](/home/tamago/ghq/github.com/tamago/orinoco-mta/docs/security/secrets_and_supply_chain.md)
