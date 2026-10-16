# 設定

`kuroshio-mta` の設定は YAML ファイルを主として管理し、必要な項目だけ環境変数で上書きする運用を想定しています。

## 基本方針

1. デフォルト値
2. 起動引数 `-config` で指定した YAML、未指定なら `MTA_CONFIG_FILE`、さらに未指定ならカレントディレクトリの `config.yaml` / `config.yml`
3. 環境変数

同じ設定を複数の方法で指定した場合は、最後に評価される環境変数が優先されます。`-config` または `MTA_CONFIG_FILE` を指定した場合は、そのパスにファイルが存在しないと起動時にエラーになります。

```bash
go run ./cmd/kuroshio -config ./config.yaml
```

サンプルは [config.example.yaml](https://github.com/tamago0224/kuroshio-mta/blob/main/config.example.yaml) にあります。

補足:
- まずは YAML にまとめて書き、環境差分や secret だけ環境変数に寄せる構成がおすすめです
- `MTA_SUBMISSION_USERS_FILE` と `MTA_ADMIN_TOKENS_FILE` は secret をファイルから読みたいときに使えます
- duration は Go の duration 形式 (`5s`, `1m`, `24h`) を使います
- 配列は YAML ではリスト、環境変数ではカンマ区切りで指定します

## 読み込みと secret

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `-` | `MTA_CONFIG_FILE` | unset | 互換用の設定ファイル指定です。通常は起動引数 `-config` を優先して使います |
| `submission_users` | `MTA_SUBMISSION_USERS` | unset | Submission 認証ユーザーを `user@example.com:password,...` 形式で指定します |
| `-` | `MTA_SUBMISSION_USERS_FILE` | unset | `MTA_SUBMISSION_USERS` の代わりにファイルから Submission 認証情報を読み込みます |
| `submission_auth_dsn` | `MTA_SUBMISSION_AUTH_DSN` | unset | `submission_auth_backend: sqlite` で使う SQLite DSN です |
| `admin_tokens` | `MTA_ADMIN_TOKENS` | unset | Admin API の Bearer token と role を `token:role,...` または `sha256=<hex>:role,...` 形式で指定します |
| `-` | `MTA_ADMIN_TOKENS_FILE` | unset | `MTA_ADMIN_TOKENS` の代わりにファイルから管理トークンを読み込みます |

補足:
- 起動引数 `-config <path>` が、設定ファイルを明示する第一選択です
- `-config` と `MTA_CONFIG_FILE` を同時に使った場合は `-config` が優先されます

## SMTP 受信と Submission

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `listen_addr` | `MTA_LISTEN_ADDR` | `:2525` | SMTP 受信サーバーの待受アドレスです |
| `submission_addr` | `MTA_SUBMISSION_ADDR` | unset | Submission リスナーの待受アドレスです。設定すると有効になります |
| `submission_auth_required` | `MTA_SUBMISSION_AUTH_REQUIRED` | `true` | Submission で認証を必須にするかを制御します |
| `submission_auth_backend` | `MTA_SUBMISSION_AUTH_BACKEND` | `static` | Submission の認証 backend を切り替えます。`static` / `sqlite` を使います |
| `submission_bearer_introspection_url` | `MTA_SUBMISSION_BEARER_INTROSPECTION_URL` | unset | 設定すると HTTPS の RFC 7662 introspection を使う XOAUTH2 を有効化 |
| `submission_bearer_client_id` | `MTA_SUBMISSION_BEARER_CLIENT_ID` | unset | introspection 用 client ID（有効時は必須） |
| `submission_bearer_client_secret` | `MTA_SUBMISSION_BEARER_CLIENT_SECRET` | unset | introspection 用 client secret（有効時は必須）。`MTA_SUBMISSION_BEARER_CLIENT_SECRET_FILE` も利用可能（両方設定時は FILE を優先） |
| `submission_bearer_issuer` | `MTA_SUBMISSION_BEARER_ISSUER` | unset | 応答の `iss` と一致させる値（有効時は必須） |
| `submission_bearer_audience` | `MTA_SUBMISSION_BEARER_AUDIENCE` | unset | 応答の `aud` に要求する値（有効時は必須） |
| `submission_bearer_username_claim` | `MTA_SUBMISSION_BEARER_USERNAME_CLAIM` | `sub` | SASL の user と一致させるメールボックス claim。opaque sub の IdP は `email` 等を指定 |
| `submission_bearer_required_scope` | `MTA_SUBMISSION_BEARER_REQUIRED_SCOPE` | `smtp.send` | 要求する単一 scope。空指定では既定値を使用 |
| `submission_enforce_sender_identity` | `MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY` | `true` | 認証ユーザーのドメインと `MAIL FROM` の整合を要求します |
| `hostname` | `MTA_HOSTNAME` | `kuroshio.local` | SMTP 応答や配送で使うホスト名です |
| `tls_cert_file` | `MTA_TLS_CERT_FILE` | unset | 受信側 TLS に使う証明書ファイルです |
| `tls_key_file` | `MTA_TLS_KEY_FILE` | unset | 受信側 TLS に使う秘密鍵ファイルです |
| `max_message_bytes` | `MTA_MAX_MESSAGE_BYTES` | `10485760` | 受信するメッセージの最大サイズです |

補足:
- `submission_auth_backend: static` では既存通り `submission_users` / `MTA_SUBMISSION_USERS(_FILE)` を使います
- `submission_auth_backend: sqlite` では `submission_credentials` テーブルを参照し、`username`, `password_hash`, `enabled`, `expires_at`, `last_auth_at` を使います
- `submission_auth_backend: sqlite` では `allowed_sender_domains` / `allowed_sender_addresses` を使って sender identity の許可範囲を広げられます
- `password_hash` は平文ではなく SHA-256 hex を保存します

## ログ・監視・運用 API

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `log_level` | `MTA_LOG_LEVEL` | `info` | JSON ログの出力レベルです。`debug` / `info` / `warn` / `error` を使います |
| `observability_addr` | `MTA_OBSERVABILITY_ADDR` | `:9090` | `/metrics` や `/slo` を公開する待受アドレスです |
| `otel_enabled` | `MTA_OTEL_ENABLED` | `false` | OpenTelemetry tracing を有効にします |
| `otel_service_name` | `MTA_OTEL_SERVICE_NAME` | `kuroshio-mta` | OTEL resource に設定する `service.name` です |
| `otel_service_version` | `MTA_OTEL_SERVICE_VERSION` | unset | OTEL resource に設定する `service.version` です |
| `otel_exporter_otlp_endpoint` | `MTA_OTEL_EXPORTER_OTLP_ENDPOINT` | unset | OTLP/HTTP trace exporter の送信先 URL です。`http://collector:4318/v1/traces` のように指定します |
| `otel_exporter_otlp_insecure` | `MTA_OTEL_EXPORTER_OTLP_INSECURE` | `false` | OTLP exporter の接続を平文 HTTP に切り替えます。HTTPS の証明書検証を省略する設定ではありません |
| `otel_trace_sample_ratio` | `MTA_OTEL_TRACE_SAMPLE_RATIO` | `1.0` | head-based sampling の比率です。`0.0` から `1.0` の範囲で指定します |
| `admin_auth_backend` | `MTA_ADMIN_AUTH_BACKEND` | `config` | 管理認証の保存先です。`config` / `sqlite` を使います |
| `admin_auth_dsn` | `MTA_ADMIN_AUTH_DSN` | unset | SQLite 管理認証の DSN です。テーブル構成は [Admin API](./runbooks/admin_api.md) を参照してください |
| `admin_addr` | `MTA_ADMIN_ADDR` | unset | Admin API の待受アドレスです。設定すると有効になります |

## キューと配送

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `queue_dir` | `MTA_QUEUE_DIR` | `./var/queue` | ローカルキューを保存するディレクトリです |
| `queue_backend` | `MTA_QUEUE_BACKEND` | `local` | キューバックエンドを切り替えます。`local` / `kafka` を使います |
| `delivery_mode` | `MTA_DELIVERY_MODE` | `mx` | 配送方式です。`mx` / `local_spool` / `relay` を使います |
| `spool_backend` | `MTA_SPOOL_BACKEND` | `local` | `local_spool` 配送時の保存先 backend です。`local` / `s3` を使います |
| `local_spool_dir` | `MTA_LOCAL_SPOOL_DIR` | `./var/spool` | `spool_backend: local` で使う保存先ディレクトリです |
| `spool_s3_bucket` | `MTA_SPOOL_S3_BUCKET` | unset | `spool_backend: s3` で使う bucket 名です |
| `spool_s3_prefix` | `MTA_SPOOL_S3_PREFIX` | unset | `spool_backend: s3` で object key の先頭に付ける prefix です |
| `spool_s3_endpoint` | `MTA_SPOOL_S3_ENDPOINT` | unset | S3-compatible object storage の endpoint です |
| `spool_s3_region` | `MTA_SPOOL_S3_REGION` | `us-east-1` | `spool_backend: s3` の region です |
| `spool_s3_access_key` | `MTA_SPOOL_S3_ACCESS_KEY` | unset | `spool_backend: s3` の access key です |
| `spool_s3_secret_key` | `MTA_SPOOL_S3_SECRET_KEY` | unset | `spool_backend: s3` の secret key です |
| `spool_s3_force_path_style` | `MTA_SPOOL_S3_FORCE_PATH_STYLE` | `false` | path-style addressing を強制します。MinIO などで使います |
| `spool_s3_use_tls` | `MTA_SPOOL_S3_USE_TLS` | `true` | S3 backend で TLS endpoint を使う想定のフラグです |
| `relay_host` | `MTA_RELAY_HOST` | unset | `relay` 配送時の中継先ホストです |
| `relay_port` | `MTA_RELAY_PORT` | `25` | `relay` 配送時の中継先ポートです |
| `relay_require_tls` | `MTA_RELAY_REQUIRE_TLS` | `false` | リレー配送で TLS を必須にします |
| `worker_count` | `MTA_WORKER_COUNT` | `4` | 配送ワーカー数です |
| `max_attempts` | `MTA_MAX_ATTEMPTS` | `12` | 最大再試行回数です |
| `max_retry_age` | `MTA_MAX_RETRY_AGE` | `120h` | 再試行を続ける最大期間です |
| `retry_schedule` | `MTA_RETRY_SCHEDULE` | `5m,30m,2h,6h,24h` | 再試行間隔です。YAML では配列、環境変数ではカンマ区切りで指定します |
| `scan_interval` | `MTA_SCAN_INTERVAL` | `5s` | キュースキャンの間隔です |
| `dial_timeout` | `MTA_DIAL_TIMEOUT` | `8s` | 配送先への接続タイムアウトです |
| `send_timeout` | `MTA_SEND_TIMEOUT` | `20s` | SMTP 送信全体のタイムアウトです |

Kafka バックエンドを使う場合の設定項目と例は [kafka_queue_mode.md](./kafka_queue_mode.md) にまとめています。

## Kafka Queue Backend

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `kafka_brokers` | `MTA_KAFKA_BROKERS` | `localhost:9092` | Kafka broker 一覧です。YAML では配列、環境変数ではカンマ区切りで指定します |
| `kafka_consumer_group` | `MTA_KAFKA_CONSUMER_GROUP` | `kuroshio-mta` | Kafka consumer group 名です |
| `kafka_topic_inbound` | `MTA_KAFKA_TOPIC_INBOUND` | `mail.inbound` | inbound メッセージの topic 名です |
| `kafka_topic_retry` | `MTA_KAFKA_TOPIC_RETRY` | `mail.retry` | retry メッセージの topic 名です |
| `kafka_topic_dlq` | `MTA_KAFKA_TOPIC_DLQ` | `mail.dlq` | dead-letter queue の topic 名です |
| `kafka_topic_sent` | `MTA_KAFKA_TOPIC_SENT` | `mail.sent` | 送信完了メッセージの topic 名です |

## 認証・ポリシー・配送安全性

レピュテーション集計は `queue_dir/reputation.json` に復元・保存します。取り込み形式、保存周期、復旧手順は [Reputation Operations](./runbooks/reputation_ops.md) を参照してください。

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `dnsbl_zones` | `MTA_DNSBL_ZONES` | unset | 参照する DNSBL zone 一覧です |
| `dnsbl_cache_ttl` | `MTA_DNSBL_CACHE_TTL` | `5m` | DNSBL 判定結果のキャッシュ期間です |
| `dane_dnssec_trust_model` | `MTA_DANE_DNSSEC_TRUST_MODEL` | `ad_required` | DANE の DNSSEC 信頼モデルです。`ad_required` / `insecure_allow_unsigned` を使います |
| `mta_sts_cache_ttl` | `MTA_MTA_STS_CACHE_TTL` | `1h` | MTA-STS policy のキャッシュ期間です |
| `mta_sts_fetch_timeout` | `MTA_MTA_STS_FETCH_TIMEOUT` | `5s` | MTA-STS policy の取得タイムアウトです |
| `dkim_sign_domain` | `MTA_DKIM_SIGN_DOMAIN` | unset | 送信時 DKIM/ARC 署名に使うドメインです |
| `dkim_sign_selector` | `MTA_DKIM_SIGN_SELECTOR` | unset | 送信時 DKIM/ARC 署名に使う selector です |
| `dkim_private_key_file` | `MTA_DKIM_PRIVATE_KEY_FILE` | unset | DKIM/ARC 共用の PEM 秘密鍵です。RSA は PKCS#1 / PKCS#8、Ed25519 は PKCS#8 に対応し、鍵種別から署名アルゴリズムを自動選択します |
| `dkim_sign_headers` | `MTA_DKIM_SIGN_HEADERS` | `from:to:subject:date:message-id` | DKIM 署名対象ヘッダです。ARC 署名も同じ鍵設定を利用します |
| `arc_failure_policy` | `MTA_ARC_FAILURE_POLICY` | `accept` | ARC 検証失敗時の扱いです。`accept` / `quarantine` / `reject` を使います |
| `spf_helo_policy` | `MTA_SPF_HELO_POLICY` | `advisory` | HELO に対する SPF 判定ポリシーです。`off` / `advisory` / `enforce` を使います |
| `spf_mailfrom_policy` | `MTA_SPF_MAILFROM_POLICY` | `advisory` | `MAIL FROM` に対する SPF 判定ポリシーです。`off` / `advisory` / `enforce` を使います |

## レート制御と配送スロットリング

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `ingress_rate_limit_per_minute` | `MTA_INGRESS_RATE_LIMIT_PER_MINUTE` | `100` | 単純な受信レート制限のベース値です |
| `rate_limit_backend` | `MTA_RATE_LIMIT_BACKEND` | `memory` | RateLimiter の状態保存先です。`memory` / `redis` を使います |
| `rate_limit_rules` | `MTA_RATE_LIMIT_RULES` | unset | イベント別レート制限ルールです。フォーマット詳細は [rate_limit.md](./rate_limit.md) を参照してください |
| `rate_limit_redis_addrs` | `MTA_RATE_LIMIT_REDIS_ADDRS` | `localhost:6379` | `rate_limit_backend: redis` のときに使う Redis/Valkey アドレスです。YAML では配列、環境変数ではカンマ区切りで指定します |
| `rate_limit_redis_username` | `MTA_RATE_LIMIT_REDIS_USERNAME` | unset | Redis/Valkey 接続に使うユーザー名です |
| `rate_limit_redis_password` | `MTA_RATE_LIMIT_REDIS_PASSWORD` | unset | Redis/Valkey 接続に使うパスワードです |
| `rate_limit_redis_db` | `MTA_RATE_LIMIT_REDIS_DB` | `0` | 単一ノード接続時に使う DB 番号です |
| `rate_limit_redis_key_prefix` | `MTA_RATE_LIMIT_REDIS_KEY_PREFIX` | `kuroshio:ratelimit` | Redis/Valkey 上で RateLimiter 状態を保存するキー prefix です |
| `domain_max_concurrent_default` | `MTA_DOMAIN_MAX_CONCURRENT_DEFAULT` | `8` | ドメインごとの同時配送数のデフォルト上限です |
| `domain_max_concurrent_rules` | `MTA_DOMAIN_MAX_CONCURRENT_RULES` | unset | ドメイン別の同時配送上限を `gmail.com:2,yahoo.com:1` 形式で指定します |
| `domain_throttle_backend` | `MTA_DOMAIN_THROTTLE_BACKEND` | `memory` | 配送側 domain throttle の状態保存先です。`memory` / `redis` を使います |
| `domain_throttle_redis_addrs` | `MTA_DOMAIN_THROTTLE_REDIS_ADDRS` | `localhost:6379` | `domain_throttle_backend: redis` のときに使う Redis/Valkey アドレスです。YAML では配列、環境変数ではカンマ区切りで指定します |
| `domain_throttle_redis_username` | `MTA_DOMAIN_THROTTLE_REDIS_USERNAME` | unset | Redis/Valkey 接続に使うユーザー名です |
| `domain_throttle_redis_password` | `MTA_DOMAIN_THROTTLE_REDIS_PASSWORD` | unset | Redis/Valkey 接続に使うパスワードです |
| `domain_throttle_redis_db` | `MTA_DOMAIN_THROTTLE_REDIS_DB` | `0` | 単一ノード接続時に使う DB 番号です |
| `domain_throttle_redis_key_prefix` | `MTA_DOMAIN_THROTTLE_REDIS_KEY_PREFIX` | `kuroshio:domainthrottle` | Redis/Valkey 上で domain throttle lease を保存するキー prefix です |
| `domain_adaptive_throttle` | `MTA_DOMAIN_ADAPTIVE_THROTTLE` | `true` | 一時失敗率に応じた自動スロットリングを有効にします |
| `domain_tempfail_threshold` | `MTA_DOMAIN_TEMPFAIL_THRESHOLD` | `0.3` | ペナルティを強める tempfail 比率のしきい値です |
| `domain_penalty_max` | `MTA_DOMAIN_PENALTY_MAX` | `5s` | 自動スロットリングで加える最大ペナルティです |

`domain_throttle_backend: redis` を使うと、複数ノード間で少なくとも次を共有できます。

- ドメインごとの同時実行 lease
- adaptive throttle の sample / tempfail 集計
- penalty 状態

実行中の Redis / Valkey エラーは、現在の実装では fail-open で扱います。

## データ保持と reputation

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `data_retention_sent` | `MTA_DATA_RETENTION_SENT` | `720h` | 送信済みデータの保持期間です |
| `data_retention_dlq` | `MTA_DATA_RETENTION_DLQ` | `2160h` | DLQ データの保持期間です |
| `data_retention_poison` | `MTA_DATA_RETENTION_POISON` | `4320h` | poison データの保持期間です |
| `retention_sweep_interval` | `MTA_RETENTION_SWEEP_INTERVAL` | `1h` | 保持期限を掃除する周期です |
| `reputation_start_date` | `MTA_REPUTATION_START_DATE` | unset | reputation 集計の開始日です。`YYYY-MM-DD` 形式を使います |
| `reputation_warmup_rules` | `MTA_REPUTATION_WARMUP_RULES` | `0:100,7:1000,14:5000` | reputation warmup の段階ルールです |
| `reputation_bounce_threshold` | `MTA_REPUTATION_BOUNCE_THRESHOLD` | `0.05` | bounce rate のしきい値です |
| `reputation_complaint_threshold` | `MTA_REPUTATION_COMPLAINT_THRESHOLD` | `0.001` | complaint rate のしきい値です |
| `reputation_min_samples` | `MTA_REPUTATION_MIN_SAMPLES` | `100` | reputation 判定を有効にする最小サンプル数です |

## 関連ドキュメント

- 設定サンプル: [config.example.yaml](https://github.com/tamago0224/kuroshio-mta/blob/main/config.example.yaml)
- Rate Limit 詳細: [rate_limit.md](./rate_limit.md)
- Kafka Queue モード詳細: [kafka_queue_mode.md](./kafka_queue_mode.md)
- 運用 API: [admin_api.md](./runbooks/admin_api.md)
- SLO runbook: [slo_backlog.md](./runbooks/slo_backlog.md), [slo_delivery.md](./runbooks/slo_delivery.md), [slo_retry.md](./runbooks/slo_retry.md)
- シークレット運用: [secrets_and_supply_chain.md](./security/secrets_and_supply_chain.md)

### XOAUTH2 の運用条件

`submission_bearer_introspection_url` は管理者が信頼する IdP の HTTPS endpoint に限定します。
client credentials は HTTP Basic、応答上限は 64 KiB、timeout は 5 秒、redirect は拒否します。
TLS 証明書はシステム trust store で検証します。private CA は OS trust store で設定してください。
応答は `active: true`、未来の `exp`、期待する `iss` / `aud` / scope、空でない `sub` と
認証 user に一致するメールボックス claim を必要とします。`nbf` が未来の場合や、
`token_type` が存在して Bearer 以外の場合も拒否します。これらは RFC の必須項目より厳しいローカルポリシーです。
IdP は access token のみをこの endpoint で有効と判定するよう設定してください。

XOAUTH2 は TLS 後の EHLO でのみ広告され、TLS 前の AUTH は 538 で拒否します。
`submission_auth_backend` のパスワード認証と併用でき、static users が空なら XOAUTH2 のみになります。
`submission_enforce_sender_identity: true` では Bearer user と同じアドレスだけを MAIL FROM に許可します。
static / sqlite の既存の送信者ポリシーは維持します。

詳細は [Submission Auth Runbook](runbooks/submission_auth.md) を参照してください。

## キュー容量とバックプレッシャー

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `queue_max_messages` | `MTA_QUEUE_MAX_MESSAGES` | `0` | 未配送件数の上限。0 は無効 |
| `queue_max_bytes` | `MTA_QUEUE_MAX_BYTES` | `0` | 未配送メッセージの `Data` バイト数の合計上限。0 は無効 |
| `queue_low_watermark_percent` | `MTA_QUEUE_LOW_WATERMARK_PERCENT` | `0` | 拒否後に再開する使用率。0 は80%、設定範囲は1〜99 |
| `queue_min_free_bytes` | `MTA_QUEUE_MIN_FREE_BYTES` | `0` | 受信 JSON の保存後にも残す空き容量。0 は無効 |

容量制限は `local` backend 専用です。有効化するとディレクトリの排他ロックを取得し、
保存直前の計測から保存完了までを直列化します。超過時は DATA の最終応答を `451` とし、
250 を返したメールを容量都合で削除しません。低水位以下に戻った後の次の投入で再開します。
`Data` は追加済みヘッダを含み、JSON の base64・メタデータ・一時ファイルはバイト上限に含みません。
ディスク保護には `queue_min_free_bytes` を併用してください。

保証範囲、Kafka での制約、監視・復旧方法は [Queue Backlog runbook](./runbooks/slo_backlog.md) を参照してください。

### 配送失敗と宛先抑止

配送失敗の分類は追加設定なしで有効です。RCPT の `5.1.1` を伴う恒久失敗のみを自動 suppression に登録し、
DATA のポリシー拒否や認証・TLSエラー、容量不足、未知の550では登録しません。
`delivery_mode: mx` / `relay` の SMTP 配送に適用し、既存の再送設定や suppression の保存形式は維持します。
分類・監視・既存抑止の確認手順は [Admin API runbook](./runbooks/admin_api.md#配送失敗分類と自動-suppression) を参照してください。

## MX / provider グループの配送制御

`mx_shaping` は YAML のネストした設定です（対応する環境変数はありません）。
`enabled: false` が既定で、`mx` / `relay` 配送時に有効化できます。

| property (`mx_shaping` 内) | default | 説明 |
| --- | --- | --- |
| `enabled` | `false` | MX/provider グループ制御を有効化 |
| `backend` | `memory` | `memory` / `redis` |
| `cache_ttl` | `0s` | 成功した MX 結果のアプリケーションキャッシュ期間（0〜1h）。0 はキャッシュなし |
| `retry_delay` | `1s` | 制限・backend 障害時の最小延期時間（0 は1s、最大1h） |
| `default` | すべて無制限 | 自動 MX グループに適用する制限 |
| `providers` | 空配列 | 管理者定義グループの順序付きリスト |

`default` / `providers[].limits` の項目:

| property | default | 説明 |
| --- | --- | --- |
| `messages_per_minute` | `0` | recipient ごとの配送試行数/分。0 は無制限 |
| `message_burst` | `1`（rate 有効時） | 通数 token bucket の容量。0 指定も1として扱う |
| `connections_per_minute` | `0` | TCP 接続開始試行数/分。0 は無制限 |
| `connection_burst` | `1`（rate 有効時） | 接続 token bucket の容量。0 指定も1として扱う |
| `max_connections` | `0` | 接続開始から終了までの同時実行数。0 は無制限 |

各数値は0〜1,000,000です。rate は時間に比例して連続補充し、burst は初期状態で満杯です。
接続に失敗した dial も接続数に含み、同一 recipient の代替 MX 試行は通数を追加消費せず接続枠のみ消費します。
受信・配送成功通数とは異なる保護用の試行数です。

`providers[]` は `name`、完全一致する `domains`、境界を考慮する `mx_suffixes`、`limits` を持ちます。
優先順位は **明示ドメイン一致 → MX suffix 一致 → 自動 MX 集合** です。
suffix が複数グループに一致する場合はリスト先頭を優先します。グループの `limits` は `default` を全体として置換し、
省略したフィールドは無制限です。既存の `domain_max_concurrent_*` と adaptive penalty は別の追加制約として維持します。

Redis は `domain_throttle_redis_addrs` / username / password / db / key_prefix を共用し、
キーはその prefix の `:mx_shaping` 名前空間に分離します。backend の選択は `mx_shaping.backend` が独立して行います。
全ノードでグループ定義・制限値・prefix・送信元 scope（初期実装は `default` の1つ）を揃えてください。

例と保証範囲、延期・監視・Redis 結合テストは
[Domain Throttle 設計・運用](./architecture/domain_throttle_externalization.md#mx-provider-グループ制御) を参照してください。
