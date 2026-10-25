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

認証必須 Submission と SMTP 配送だけで構成された環境では RFC 8689 `REQUIRETLS` を広告します。受理した要求はキューと再送で保持され、STARTTLS と REQUIRETLS の両方を提供する next hop にだけ配送します。詳細と対象外経路は [RFC 8689 Gap Note](./rfc_8689_gap.md) を参照してください。

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `listen_addr` | `MTA_LISTEN_ADDR` | `:2525` | SMTP 受信サーバーの待受アドレスです |
| `submission_tls_addr` | `MTA_SUBMISSION_TLS_ADDR` | unset | 暗黙 TLS の Submission 待受（例 `:465`）。証明書・秘密鍵が必須。既存待受と併用可能 |
| `submission_allow_insecure_auth` | `MTA_SUBMISSION_ALLOW_INSECURE_AUTH` | `false` | 隔離した検証環境のみで PLAIN/LOGIN の平文認証を許可 |
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

### 認証付き HTTP メール投入

`http_injection.enabled: true` にすると、`POST /api/v1/messages` を専用 HTTPS listener で公開します。SMTP Submission と同じ static / SQLite 認証情報、sender scope、`max_message_bytes`、`smtp_limits.max_recipients` を使用します。Admin API token は送信権限として使用しません。

| property | default | 説明 |
| --- | --- | --- |
| `enabled` | `false` | HTTP 投入 API を有効化 |
| `listen_addr` | `127.0.0.1:8443` | HTTPS listener。公開範囲は明示的に設定する |
| `idempotency_ttl` | `24h` | principal と `Idempotency-Key` の組を保持する期間（1分〜30日） |
| `rate_limit_per_minute` | `60` | process ごとの認証 principal 単位の投入上限 |

有効時は `tls_cert_file` と `tls_key_file` が必須です。リクエスト形式、認証、再送契約、curl 例は [HTTP Message Injection](./runbooks/http_injection.md) を参照してください。

### 配送側 TLS-RPT

`tls_report.enabled: true` は、MTA-STS / DANE を適用した外向き SMTP セッションの成功・TLS失敗を UTC 日単位で集約し、宛先 domain の `_smtp._tls` TXT policy が指定する HTTPS endpoint へ RFC 8460 JSON を送ります。受信した TLS-RPT を扱う reputation store とは別の状態です。

| property | default | 説明 |
| --- | --- | --- |
| `enabled` | `false` | 配送側 TLS report の集約・送信を有効化 |
| `organization` | unset | report の `organization-name` |
| `contact_info` | unset | report 運用連絡先の addr-spec |
| `report_domain` | unset | report ID を生成する送信者 domain |
| `flush_interval` | `1h` | 完了した UTC 日の送信確認間隔（1分〜24時間） |
| `max_records` | `100000` | process が保持する集約tuple上限 |
| `max_report_bytes` | `5242880` | 非圧縮 JSON report の上限 |

初期実装は HTTPS transport のみです。`mailto:` は DKIM署名とreport配送時のTLS policy bypassを安全に分離するまで無視します。配送・再送・SSRF制約は [Outgoing TLS-RPT](./runbooks/outgoing_tls_reports.md) を参照してください。

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
| `delivery_routes` | YAML only | unset | 宛先ドメイン別の名前付き配送ルートです。配列順で最初に一致したルートを使います |
| `egress_sources` | YAML only | unset | 送信元IP、EHLO名、選択weight、日次上限、停止状態を定義します |
| `egress_pools` | YAML only | unset | 名前付き送信元poolと所属sourceを定義します |
| `egress_tenant_pools` | YAML only | unset | 正規化済みtenantから送信元poolへの上書きmappingです |
| `worker_count` | `MTA_WORKER_COUNT` | `4` | 配送ワーカー数です |
| `max_attempts` | `MTA_MAX_ATTEMPTS` | `12` | 最大再試行回数です |
| `max_retry_age` | `MTA_MAX_RETRY_AGE` | `120h` | 再試行を続ける最大期間です |
| `retry_schedule` | `MTA_RETRY_SCHEDULE` | `5m,30m,2h,6h,24h` | 再試行間隔です。YAML では配列、環境変数ではカンマ区切りで指定します |
| `scan_interval` | `MTA_SCAN_INTERVAL` | `5s` | キュースキャンの間隔です |
| `dial_timeout` | `MTA_DIAL_TIMEOUT` | `8s` | 配送先への接続タイムアウトです |
| `send_timeout` | `MTA_SEND_TIMEOUT` | `20s` | SMTP 送信全体のタイムアウトです |

### SMTP 接続プール

`smtp_connection_pool` は送信側 SMTP 接続の再利用を制御する YAML 専用設定です。既定では無効で、無効時は従来どおり配送ごとに接続して QUIT します。

| property | default | 説明 |
| --- | --- | --- |
| `enabled` | `false` | 接続再利用を有効化 |
| `max_connections` | `64` | pool 全体の接続上限（使用中と idle の合計） |
| `max_connections_per_key` | `4` | 接続キーごとの上限 |
| `max_idle` | `32` | pool 全体で保持する idle 接続上限。0 は保持しない |
| `max_idle_per_key` | `2` | 接続キーごとの idle 上限。0 は保持しない |
| `idle_timeout` | `30s` | 再利用可能な idle 時間（最大1時間） |
| `max_lifetime` | `5m` | 接続の最大寿命（最大24時間） |
| `max_deliveries` | `100` | 1接続で完了できる配送数 |

接続キーには接続先 host/port、送信元 IP、EHLO 名、relay 認証 identity、TLS 必須条件と DANE TLSA 条件が含まれます。異なるキー間では接続を共有しません。接続は1配送が排他的に借用し、421、EOF、通信エラー、RSET 失敗、期限・回数超過では破棄します。DATA を送信し終えた後に最終応答を読めない場合は、重複配送の可能性を表す `SMTP delivery status unknown after DATA` として DLQ に移し、自動再送を抑止します。

監視には `smtp_pool_dialed_total`、`smtp_pool_reused_total`、`smtp_pool_closed_total`、`smtp_pool_tls_handshakes_total` と `smtp_pool_open_connections` / `smtp_pool_idle_connections` を使います。再利用率は `reused / (dialed + reused)` で計算できます。設定判断と計測方法は [SMTP connection pool](./architecture/smtp_connection_pool.md) を参照してください。

### 配送イベント Webhook

`event_webhook` は受理・配送結果を永続 outbox から単一 endpoint へ通知する YAML 専用設定です。既定では無効です。

| property | default | 説明 |
| --- | --- | --- |
| `enabled` | `false` | 永続イベント通知を有効化 |
| `url` | unset | HTTPS endpoint。userinfo、query、fragment、redirect は許可しない |
| `secret_file` | unset | HMAC-SHA256 用の32〜4096 byteの単一行 secret |
| `allow_http_loopback` | `false` | テスト時のみ literal loopback IP への HTTP を許可 |
| `timeout` | `10s` | 1回の HTTP request 上限（最大30秒） |
| `scan_interval` | `1s` | outbox scan 間隔（最大1分） |
| `max_pending` | `100000` | pending event 上限（最大100万） |
| `max_attempts` | `12` | DLQ 移動までの試行上限（最大100） |
| `max_retry_delay` | `1h` | exponential backoff 上限（最大24時間） |
| `retention` | `168h` | pending / DLQ 保持期限（最大365日） |

署名、schema、HTTP status ごとの再送判断、クラッシュ境界、PII、secret rotation、監視方法は [Delivery Event Webhooks](./architecture/delivery_event_webhooks.md) を参照してください。

### traffic class・tenant公平配送

`queue_fairness.enabled: true` は、最大 `worker_count * 4 * prefetch_factor` 件を先読みし、
class weightとtenant単位のround-robinで各batchを選びます。既定は無効です。

```yaml
queue_fairness:
  enabled: true
  prefetch_factor: 8
  max_tenants: 1024
  tenant_max_per_batch: 2 # 0はbatch単位のhard capなし
  class_weights: {transactional: 4, bulk: 1, system: 1, default: 1}
  class_max_per_batch: {bulk: 4}
  tenant_classes: {newsletter@example.test: bulk}
```

認証済みSubmissionだけが正規化済み認証usernameをtenantとして取得し、既定で`transactional`になります。
`tenant_classes` は管理者がtrusted tenantをclassへ割り当てます。SMTPヘッダからtenant/classを読み取りません。
未認証SMTPと属性のない旧JSONは`default`、内部生成するDSN/DMARC集約reportは`system`です。
未知classは`default`へfallbackし、`max_tenants`到達後の未知tenantも`default`へ集約します。

classは最大16、tenant mappingは`max_tenants`以下、weightは1〜100、prefetch factorは2〜64です。
weightは相対比率で、`tenant_max_per_batch`と`class_max_per_batch`は1回のscanに対するhard capです。
低優先classをweight 0で停止する設定は許可しません。Local/Kafkaとも同じmessage JSON属性を使い、retryと
再起動後も保持します。再起動時のscheduler cursorは初期化されますが、message属性は失われません。

`queue_fairness_buffered_messages`、`queue_fairness_active_tenants`、`queue_fairness_max_tenant_depth`、
`queue_fairness_oldest_age_seconds`、`queue_fairness_class_<class>_depth`で先読みbufferを監視します。
`queue_fairness_selected_class_<class>_total`で実際のbatch選択数を確認します。
tenant名はPII/cardinalityを避けるためmetric labelや名前へ出しません。

Kafka バックエンドを使う場合の設定項目と例は [kafka_queue_mode.md](./kafka_queue_mode.md) にまとめています。

### 宛先別の名前付き配送ルート

`delivery_routes` は宛先ドメインの完全一致とサブドメイン一致を上から順に評価します。
一致しない宛先は `delivery_mode` とトップレベルの relay / spool 設定へフォールバックします。
同じメッセージの宛先が異なるルートに一致した場合も宛先ごとに配送し、成功済みの宛先を
retry レコードから除外します。local / Kafka のどちらのキューでも、再試行に残す `rcpt_to` は
未配送の宛先だけです。

```yaml
delivery_mode: mx
delivery_routes:
  - name: internal-relay
    domains: [corp.example]
    mode: relay
    relay_host: smtp.internal.example
    relay_port: 587
    relay_require_tls: true
    relay_username: kuroshio
    relay_password_file: /run/secrets/internal-relay-password
    egress_pool: transactional
  - name: archive-spool
    domains: [archive.example]
    mode: local_spool
  - name: dovecot-mailstore
    domains: [mailbox.example]
    mode: lmtp
    lmtp_unix_socket: /run/dovecot/lmtp
```

route の `mode` は `mx` / `local_spool` / `relay` / `lmtp` です。認証付き relay は STARTTLS 後に
サーバー証明書とホスト名を検証してから SMTP AUTH PLAIN を送ります。平文接続への資格情報送信や
証明書検証の無効化は設定できません。`relay_username` と `relay_password_file` は一組で指定し、
`relay_require_tls: true` が必要です。password file は起動時に読み込み、空ファイルや読込失敗では
起動しません。ファイルを0600など配送プロセスだけが読める権限で管理してください。

ルート設定と資格情報は起動時のスナップショットです。SIGHUP の限定的な再読込対象ではないため、
変更後はプロセスを再起動します。キュー内のメッセージはルート名を固定保存せず、各配送試行時の
設定で選び直します。このため、再起動後の retry は新しいルート定義を使います。

### 送信元IP・EHLO poolとWarm-up上限

`egress_sources` と `egress_pools` は、`mx` / `relay` 配送で使うローカル送信元を選択します。
route の `egress_pool` を基準にし、メッセージにtenantがある場合は `egress_tenant_pools` の完全一致を優先します。
どちらにもpool指定がない既存設定は、従来どおりOSが選ぶ送信元IPとトップレベルの `hostname` を使います。

```yaml
egress_sources:
  - name: primary
    ip: 192.0.2.10
    ehlo: mail.example.com
    weight: 4
    daily_limit: 10000
  - name: warmup
    ip: 192.0.2.11
    ehlo: warmup.example.com
    weight: 1
    daily_limit: 1000
    stopped: false
egress_pools:
  - name: transactional
    sources: [primary, warmup]
egress_tenant_pools:
  alice@example.com: transactional
```

weightはpool内の相対的な選択比率で1〜100、`daily_limit` はsourceごとの配送試行予約数です。
0は上限なしを表し、UTCの日付変更で集計をリセットします。選ばれたsourceが上限到達または
`stopped: true` の場合、その配送を翌UTC日まで保留します。同じ試行で別sourceへ切り替えて制限を
迂回しません。予約数は `queue_dir/egress-source-usage.json` に0600で同期保存し、再起動後も復元します。
同じファイルを複数プロセスから共有しないでください。queue backup / restore の対象に含まれます。

設定したIPが起動時にローカルinterfaceへ割り当てられていない場合は起動に失敗します。配送先の
IPv4/IPv6とsourceのfamilyが一致しない場合も配送を開始せずretryにします。各sourceのIPには正逆DNS、
EHLO、SPF、DKIMの整合を用意し、段階的な上限を本番投入前に設定してください。選択数は
`egress_source_<source>_selected` counterで確認できます。source名はmetric名向けに正規化されます。

名前付きルートの選択は `delivery_route_selected` 監査イベントに、メッセージID、マスク済み宛先、
ルート名、配送方式を記録します。OpenTelemetry の `delivery.deliver` span には
`delivery.route` と `delivery.mode` を付与します。資格情報はログと trace に出しません。

`lmtp` はローカルの信頼境界内にある UNIX socket だけをサポートします。クライアントは `LHLO` を使い、
同じrouteの複数宛先を1トランザクションへまとめ、DATA後の宛先別応答を成功・一時失敗・恒久失敗として
個別に処理します。一時失敗した宛先だけをretryへ保存するため、local / Kafkaのいずれでも成功済み宛先を
再送しません。TCP LMTP、暗黙TLS、STARTTLSは未対応です。

Dovecotでは、例として `service lmtp` の `unix_listener /run/dovecot/lmtp` を有効にし、socketのowner/groupを
`kuroshio` プロセスの実行ユーザーが接続できる値へ設定します。socketをコンテナ間で共有する場合は専用volumeを
使い、ホスト全体から書き込める権限にしないでください。起動前に次のように接続権限を確認できます。

```bash
test -S /run/dovecot/lmtp
sudo -u kuroshio test -w /run/dovecot/lmtp
```

実メールストアとの任意結合テストは、テスト用メールボックスを用意して次を実行します。

```bash
MTA_TEST_LMTP_UNIX_SOCKET=/run/dovecot/lmtp \
MTA_TEST_LMTP_RECIPIENT=user@mailbox.example \
go test -tags=integration ./integration -run '^TestLMTPMailstore$' -v
```

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
| `dns_cache_enabled` | `MTA_DNS_CACHE_ENABLED` | `false` | プロセス内のMX・配送先アドレスキャッシュを有効にします |
| `dns_cache_positive_ttl` | `MTA_DNS_CACHE_POSITIVE_TTL` | `1m` | 成功したMX・アドレス結果を保持する運用TTLです |
| `dns_cache_negative_ttl` | `MTA_DNS_CACHE_NEGATIVE_TTL` | `30s` | 失敗したMX・アドレス結果を保持する運用TTLです |
| `dns_cache_capacity` | `MTA_DNS_CACHE_CAPACITY` | `10000` | MXとアドレスの各キャッシュに保持する最大名数です |
| `dane_dnssec_trust_model` | `MTA_DANE_DNSSEC_TRUST_MODEL` | `ad_required` | DANE の DNSSEC 信頼モデルです。`ad_required` / `insecure_allow_unsigned` を使います |
| `mta_sts_cache_ttl` | `MTA_MTA_STS_CACHE_TTL` | `1h` | MTA-STS policy のキャッシュ期間です |
| `mta_sts_fetch_timeout` | `MTA_MTA_STS_FETCH_TIMEOUT` | `5s` | MTA-STS policy の取得タイムアウトです |

### MX・配送先アドレスキャッシュ

`dns_cache_enabled: true` は、同じ宛先へのMX検索と、SMTP接続前のA/AAAA検索をプロセス内で共有します。
同じ名前への同時missは1回の問い合わせへまとめ、成功と失敗を別々のTTLで保持します。容量到達時は
期限切れまたは最終利用時刻が古い項目を削除します。期限切れ情報を障害時に返すstale fallbackは行いません。

Goのsystem resolverは応答レコードのTTLを公開しないため、ここで指定するTTLはDNS authoritative TTLではなく
運用上の上限です。MX切替を短時間で反映する必要がある環境では短い値を使うか、この機能を無効にして
TTLを扱える外部キャッシュresolverを利用してください。キャッシュは再起動時に空になり、永続化しません。

DANEのTLSA検索とDNSSEC AD判定はこのキャッシュを通らず、既存の信頼モデルを維持します。MX/A/AAAAの
キャッシュ結果からDANEの検証済み状態を推測しません。

`dns_cache_mx_{hit,miss,coalesced,error}_total` と
`dns_cache_address_{hit,miss,coalesced,error}_total` が要求数と結果を示します。
`dns_cache_{mx,address}_lookup_milliseconds_total` は実問い合わせ時間の累計で、miss件数と組み合わせて
平均レイテンシを比較できます。外部resolver側のrequest/latencyも併せて確認して有効化を判断してください。
| `dkim_profiles_file` | `MTA_DKIM_PROFILES_FILE` | unset | 複数ドメイン用の署名 profile JSON。従来の domain/selector/key 設定と併用不可 |
| `dkim_sign_domain` | `MTA_DKIM_SIGN_DOMAIN` | unset | 送信時 DKIM/ARC 署名に使うドメインです |
| `dkim_sign_selector` | `MTA_DKIM_SIGN_SELECTOR` | unset | 送信時 DKIM/ARC 署名に使う selector です |
| `dkim_private_key_file` | `MTA_DKIM_PRIVATE_KEY_FILE` | unset | DKIM/ARC 共用の PEM 秘密鍵です。RSA は PKCS#1 / PKCS#8、Ed25519 は PKCS#8 に対応し、鍵種別から署名アルゴリズムを自動選択します |
| `dkim_sign_headers` | `MTA_DKIM_SIGN_HEADERS` | `from:to:subject:date:message-id` | DKIM 署名対象ヘッダです。ARC 署名も同じ鍵設定を利用します |
| `arc_failure_policy` | `MTA_ARC_FAILURE_POLICY` | `accept` | ARC 検証失敗時の扱いです。`accept` / `quarantine` / `reject` を使います |
| `spf_helo_policy` | `MTA_SPF_HELO_POLICY` | `advisory` | HELO に対する SPF 判定ポリシーです。`off` / `advisory` / `enforce` を使います |
| `spf_mailfrom_policy` | `MTA_SPF_MAILFROM_POLICY` | `advisory` | `MAIL FROM` に対する SPF 判定ポリシーです。`off` / `advisory` / `enforce` を使います |
| `dmarc_policy_mode` | `MTA_DMARC_POLICY_MODE` | `shadow` | DMARC評価エンジンです。`legacy` はRFC 7489互換のみ、`shadow` はRFC 9989を並行観測、`enforce` はRFC 9989結果を本判定に使います |

`dkim_sign_domain` / `dkim_sign_selector` / `dkim_private_key_file` のいずれかを指定した場合は、
DKIM と ARC の署名初期化が必須です。項目の不足、鍵の欠落・読込失敗・不正形式があれば
worker 初期化で起動を停止します。未署名での配送には切り替えません。
3項目すべてが未設定の場合は従来どおり署名せず配送します。
起動前には `-check-config` で確認し、初期化に失敗した場合は設定・鍵ファイルを修復して再起動してください。
稼働後の鍵再読込で失敗した場合も、その署名・配送はエラーになります。
複数ドメインの profile 選択と DNS 事前公開を検証する切替は、
[DKIM 署名 profile と鍵切替](./runbooks/dkim_rotation.md) を参照してください。

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

### SMTP応答による期限付き自動停止

`response_throttle` は、明示した一時応答だけを時間窓内で数え、閾値到達時に宛先domainを期限付きで停止します。
既定は無効です。有効化する場合も最初は `dry_run: true` でmatch/triggerメトリクスを確認してください。

```yaml
response_throttle:
  enabled: true
  dry_run: true
  rules:
    - id: provider-rate-limit
      domains: ["example.net"] # `*` は全domain。外部提供者の確認済み応答に限定する
      smtp_code: 421
      enhanced_status: "4.7.0" # 省略時はcodeとstageだけで照合
      stage: greeting
      threshold: 5
      window: 1m
      pause: 10m
```

rule IDは一意、SMTP codeは一時失敗の400〜499、thresholdは1〜10000、window/pauseは24時間以内です。
複数ruleは記載順に照合し、最初に一致したruleだけで集計します。
match数はプロセス内の移動時間窓で保持され、再起動時に空になります。発火後の停止状態は
`delivery_pause` のlocal/Redis backendへ保存されるため再起動後も維持され、Redis利用時は全nodeから同じ停止を参照します。
複数nodeで厳密な合計閾値が必要な場合は外部集計を使い、この機能はdry-runのまま運用してください。

自動停止のactorは `auto:response-throttle:<rule-id>`、reasonはrule IDを含みます。既存の手動停止を
上書きせず、自動解除も行いません。operatorは既存の配送停止APIで一覧・解除できます。
`dry_run: false` には `delivery_pause.enabled: true` が必要です。
`worker_response_throttle_match_<rule-id>_total`、`worker_response_throttle_trigger_<rule-id>_total`、
`worker_response_throttle_error_total` を監視します。この判定はSMTP応答の恒久/一時分類を書き換えません。

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

## SMTP HTTP ポリシーフック

`policy_hook` は設定ファイルを管理できる運用管理者が接続先を指定する機能です。
独自 API `kuroshio.smtp-policy.v1` を使い、Stalwart MTA Hooks とは互換ではありません。
RCPT の既存チェック後と、DATA のメール認証・ヘッダ追加後／キュー保存前に呼び出します。

| property (`policy_hook` 内) | default | 説明 |
| --- | --- | --- |
| `url` | 空（無効） | 固定の HTTPS endpoint。SMTP 入力から変更されません |
| `timeout` | `5s` | 1回の呼び出し全体の期限。0 は5s、上限30s |
| `max_concurrent` | `32` | listener ごとの同時呼び出し数。0 は32、上限1000 |
| `max_response_bytes` | `65536` | HTTP 応答本文の上限。0 は64KiB、上限1MiB |
| `send_body` | `false` | DATA の最終メッセージ（追加ヘッダ込み）を base64 で送信する明示同意 |
| `token_file` | 空 | 任意の Bearer token ファイル。`MTA_POLICY_HOOK_TOKEN_FILE` が優先 |
| `allow_http_loopback` | `false` | HTTP をリテラル loopback IP に限って許可するローカル検証用例外 |

空の `url` では呼び出し・token 読み込みを行わず、従来の受信動作を維持します。
通常は OS trust store で証明書・ホスト名を検証する TLS 1.2 以上を使います。
private CA は OS trust store（または Go が参照する `SSL_CERT_FILE`）へ設定してください。TLS 検証を無効化する設定はありません。
redirect と環境変数由来の HTTP proxy は使いません。HTTP 応答ヘッダにも64KiBの上限を設けます。URL の userinfo・query・fragment は拒否し、
認証情報は endpoint の URL に埋め込まず token ファイルを使います。ファイルは起動時に読み、最大4KiBの非空1行です。
末尾の改行を除去し、ファイル内容を設定・ログ・SMTP 応答へ出しません。更新の反映には再起動が必要です。

認証済み管理者による設定ファイル・環境変数・secret mount の配置を前提とし、管理 API から endpoint を変更する機能はありません。
接続先は管理者の信頼境界に含まれます。外部サービスには envelope、接続元IP、HELO、TLS/Submission/認証済みフラグを送信します。
SMTP AUTH のパスワード・access token・認証ユーザー名は送信しません。`send_body: false` ではヘッダ・本文を一切送信しません。
有効時は本文自体に機密情報が含まれ得るため、サービス側の保存・ログ方針も運用管理者が設定してください。

応答は HTTP 200 の厳格な JSON で、同じ `version` と `action: accept|reject|tempfail|quarantine` のみを許可します。
`quarantine` は DATA 専用で、後述の `quarantine.enabled: true` が必要です。
RCPT での quarantine、または無効時の quarantine 応答は451にします。
RCPT reject は550、DATA reject は554、tempfail・timeout・通信失敗・TLS失敗・redirect・不正JSON・応答超過・過負荷は451です。
SMTP 応答文は固定で、サービス側が本文・宛先・ヘッダ・SMTP 応答を変更する機能はありません。
accept はフック段階のチェックを通過するだけで、既存の AUTH / SPF / DKIM / DMARC / ARC、サイズ、8BITMIME、キュー容量制限を解除しません。
DATA フックの拒否・一時失敗後はトランザクションを終了します。再試行は MAIL FROM から開始してください。

SMTP と Submission はそれぞれ独立した呼び出し枠を持つため、両方の有効化時は最大で2倍の同時数になります。
サービス過負荷時は枠待ちで滞留させず即時451にし、hook 応答を待つ時間も timeout で制限します。
`smtp_policy_hook_<rcpt|data>_<accept|reject|tempfail|quarantine>_total` と
`smtp_policy_hook_<stage>_error_<reason>_total` を監視してください。
reason は `overload`、`timeout`、`transport`、`http_status`、`read`、`response_too_large`、`json`、`version`、`action`、`action_stage`、`request`、`canceled` の固定値です。
URL・応答本文・token・メール本文はフックのメトリクスやエラー記録に含みません。

[モックサービス付き tutorial](./tutorials/policy-hook.md) と
[request schema](../examples/tutorials/policy-hook/request.schema.json) /
[response schema](../examples/tutorials/policy-hook/response.schema.json) を参照してください。

## Submission の TLS と移行

PLAIN/LOGIN は既定で TLS 前の EHLO に広告せず、直接 AUTH を送っても
資格情報のデコード・認証 backend 呼び出し・334 challenge より前に538で拒否します。
STARTTLS 後は再 EHLO が必要です。XOAUTH2 は例外設定にかかわらず TLS が必須です。

`submission_addr` は STARTTLS、`submission_tls_addr` は接続直後に TLS handshake を行います。
両者は同じ認証 backend、認証必須設定、sender policy、証明書を使用します。
暗黙 TLS は SMTP greeting 前に handshake を完了し、失敗時は接続を閉じます。
handshake の期限は30秒、最低 TLS バージョンは1.2です。平文に切り替えて継続しません。
HTTP ポリシーフックの同時実行枠は SMTP / STARTTLS Submission / 暗黙 TLS Submission の
各 listener に独立して適用され、3つ有効なら合計最大3倍です。

既存の平文 Submission 利用者は、サーバーに `tls_cert_file` / `tls_key_file` を設定し、
クライアントを証明書検証付き STARTTLS 必須または暗黙 TLS へ変更してください。
Submission 有効時は証明書の欠落・片側のみの指定・不正な鍵ペアを設定読込時に拒否します。
暗黙 TLS と他の SMTP 待受に同一アドレスを指定する設定も拒否します。

```yaml
submission_addr: ":587"
submission_tls_addr: ":465"
submission_allow_insecure_auth: false
tls_cert_file: /etc/kuroshio/tls/fullchain.pem
tls_key_file: /etc/kuroshio/tls/privkey.pem
```

`submission_allow_insecure_auth: true` は TLS 設定のない隔離検証用の互換例外です。
外部公開・実運用では使用しないでください。TLS 終端 proxy の背後でも、接続の TLS 状態を
外部ヘッダから信頼することはありません。暗黙 TLS の証明書要件はこの例外でも解除しません。

## 受信認可とローカル宛先

`inbound_policy` は YAML 専用の設定です（対応する環境変数はありません）。
省略時はローカル宛先・信頼 CIDR とも空で、未認証の中継をすべて拒否します。
loopback やプライベート IP を暗黙に信頼しません。`delivery_mode` や relay host を
設定しても受信認可は変わりません。

| property (`inbound_policy` 内) | default | 説明 |
| --- | --- | --- |
| `local_domains` | `[]` | ローカル受信対象の完全一致ドメイン。ワイルドカード・末尾ドットは不可 |
| `local_recipients` | `[]` | 対象ドメイン内で存在を認める完全一致メールボックスの静的表 |
| `trusted_cidrs` | `[]` | 外部宛への中継を許可する TCP 接続元 IPv4/IPv6 CIDR |

ドメインとメールボックスは既存の envelope 正規化に合わせて大文字小文字を区別しません。
local_domains のサブドメインは自動では含まれません。アドレスや CIDR の不正値、
local_domains に属さない local_recipients は起動前に拒否します。更新反映には再起動が必要です。

```yaml
inbound_policy:
  local_domains: [example.com]
  local_recipients: [user@example.com, postmaster@example.com]
  trusted_cidrs: [192.0.2.10/32, '2001:db8::10/128']
```

RCPT の構文検証後、HTTP ポリシーフックより前に次の順で判定します。

1. ローカルドメイン宛は静的表にあれば受理し、なければ `550 5.1.1`。
2. 外部ドメイン宛は、認証済み Submission または信頼 CIDR 内の接続元なら受理。
3. それ以外は `550 5.7.1 relaying denied`。

信頼 CIDR や認証済みユーザーも、未知のローカル宛先は受理しません。
`RCPT TO:<postmaster>` は hostname で補完されるため、受信する場合はそのドメインと
`postmaster@<hostname>` を表へ登録してください。表は受理する宛先の定義であり、
メールボックス作成やローカル配送先の選択は行いません。配送は既存の delivery_mode に従います。

失敗した AUTH・EHLO 名・MAIL FROM の自己申告・本文ヘッダ・PROXY コマンドは中継権限を与えません。
PROXY protocol 無効時は TCP peer の実アドレスだけを照合します。
有効時は信頼 proxy が伝達した client IP を照合します（下記参照）。
認証必須の Submission は信頼 CIDR でも AUTH を省略できず、sender identity 制約も維持します。
HTTP フックの accept 応答はこの認可を解除できません。

初期実装の宛先表は起動時に検証してメモリに保持し、受信中に外部 DB を参照しません。
内部の宛先検証処理が障害を返す場合は451とし、宛先を追加せず送信元の再試行に委ねます。
`smtp_reject_relay_denied_total`、`smtp_reject_unknown_recipient_total`、
`smtp_reject_recipient_backend_unavailable_total` で拒否を観測できます。

既存環境は、受け取るドメイン・メールボックスを表に列挙するか、クライアントを TLS と
認証を使う Submission に移行してください。内部 relay は実際の送信元だけを狭い CIDR で指定します。
`0.0.0.0/0` や `::/0` を指定すると公開中継を許すため使用しないでください。
公開前に、外部の未認証クライアントで外部宛 RCPT が550、既知のローカル宛が250、未知のローカル宛が550に
なることを確認し、認証失敗後にも中継できないことを確認します。
検証用 Docker tutorial は loopback へのポート公開と明示的な Docker CIDR 許可を使います。
異なる Docker subnet を使う場合は各 config.yaml を合わせ、検証用 CIDR を本番へコピーしないでください。

## TLS 証明書の無停止更新

`tls_cert_file` / `tls_key_file` は起動時に検証し、稼働中は1分ごとに再読込します。
有効な鍵ペアのみを新規 TLS handshake に反映し、失敗時は直前の証明書を保持します。
SMTP / Submission STARTTLS と暗黙 TLS が対象です。
配置・ACME staging・期限監視は [TLS 証明書更新 runbook](./runbooks/tls_certificate_rotation.md) を参照してください。

## 信頼した TCP proxy と PROXY v2

`proxy_protocol` は YAML 専用で、既定ではすべて無効です。
有効にする listener は LB 専用になり、通常の直接接続と共用できません。

```yaml
proxy_protocol:
  smtp: true
  submission: false
  submission_tls: false
  trusted_cidrs: [192.0.2.10/32, '2001:db8::10/128']
```

`smtp` は `listen_addr`、`submission` は `submission_addr`、
`submission_tls` は `submission_tls_addr` を個別に選択します。
少なくとも1つ有効なら `trusted_cidrs` は必須です。CIDR の不正値は起動前に拒否します。
反映には再起動が必要です。既存 tutorial の直接接続設定では有効にしません。

信頼 CIDR 内の実 TCP peer に限って、SMTP greeting / TLS handshake より前に
PROXY v2 を読みます。信頼外・不正・上限超過・5秒以内に読めない接続は応答せず閉じます。
PROXY v1、自動判別、UDP、UNIX、PROXY コマンドの UNSPEC は受け付けません。
IPv4/IPv6 の TCP 送信元 IP/port を採用し、宛先アドレスは経路・認可に使いません。
LOCAL は LB 自身の接続として扱い、TCP peer を維持します。
送信元が unspecified / multicast の PROXY 接続は拒否します。
フレーム payload は最大4096バイトで、TLV は長さのみ検証して無視します。
TLS TLV によって TLS 済み扱いにせず、AUTH の TLS 要件も維持します。

SPF、DNSBL、IP rate limit、HTTP policy hook、Received、受信認可は同じ client IP を使います。
実 TCP peer と伝達 client は接続内部で区別して保持します。
`proxy_protocol.trusted_cidrs` はアドレス情報を伝達する権限だけを与えます。
中継権限を与える `inbound_policy.trusted_cidrs` と混同しないでください。
LB のアドレスを中継許可表へ追加する必要はありません。

LB 側の条件・検証・監視は [PROXY v2 runbook](./runbooks/proxy_protocol.md) を参照してください。

## SMTP 受信資源の上限

`smtp_limits` は YAML 専用です。省略・0 は下表の既定値、負数は起動時エラーです。
SMTP、STARTTLS Submission、暗黙 TLS Submission の各 listener が独立して適用します。
複数プロセス・ノード間で共有する上限ではありません。変更反映には再起動が必要です。

| property | default | 範囲・動作 |
| --- | --- | --- |
| `max_connections` | `1024` | listener 全体の同時接続数。PROXY/TLS 処理中も含む |
| `max_connections_per_ip` | `32` | 実効クライアント IP ごとの同時接続数 |
| `max_recipients` | `100` | 1トランザクションで受理する RCPT 数。重複も数える |
| `max_messages` | `100` | 1接続でキュー保存に成功した通数 |
| `max_bytes` | `104857600` | 1接続のネットワーク入力バイト数。SMTP コマンド・DATA・TLS framing/handshake を含む。PROXY ヘッダは別の4096バイト上限 |
| `max_auth_failures` | `5` | 1接続の資格情報不一致・SASL 構文エラーの累計 |
| `max_rcpt_failures` | `20` | 1接続の不受理 RCPT の累計。構文・認可・フック・宛先上限の拒否も含む |
| `idle_timeout` | `5m` | ネットワーク read/write ごとの無通信期限 |
| `max_lifetime` | `10m` | 接続開始からの絶対期限。PROXY/TLS 処理時間も含む |

接続上限時は greeting 前に接続を閉じます。全体の枠は handler goroutine 起動前に確保し、
IP 別の枠は信頼済み PROXY v2 のクライアント IP 復元後、TLS handshake 前に確保します。
PROXY の読込中も全体上限と既存の5秒期限で保護します。
枠は切断・キャンセル・TLS失敗で解放し、STARTTLS 中は保持します。
LB が同じ IP を使う場合は [PROXY v2](runbooks/proxy_protocol.md) と組み合わせてください。

宛先上限は `452 4.5.3` を返し、受理済み宛先への DATA は継続できます。
通数・AUTH/RCPT 失敗回数の上限では、直前コマンドの応答後に `421` を返して閉じます。
成功済みの `250 queued` は取り消しません。RSET、再 EHLO、STARTTLS は接続累計をリセットしません。
AUTH の TLS 前拒否・未対応 AUTH・認証 backend 未設定は資格情報の失敗数に含めません。
転送量上限ではコマンド待受中は421、DATA中は451の後に閉じます。
無通信・寿命期限では切断します。DATA の行長・サイズ超過や読込失敗も、
残りの本文をコマンドとして解釈しないよう応答後に切断します。
無通信期限は各ネットワーク I/O で更新しますが、少量ずつ送るクライアントにも絶対期限は延長しません。

運用では `smtp_active_connections_<smtp|submission|submission_tls>` を
`smtp_connection_limit_<listener>` と比較して接続使用率を監視します。
`smtp_limit_<connections|connections_per_ip|recipients|messages|bytes|auth_failures|rcpt_failures|timeout>_total`
は制限の拒否・切断回数です（handshake/PROXY 段階の失敗は各既存メトリクスも参照）。
IP をメトリクス名に含めません。接続元の再送・正当な一括投入を確認してから上限を調整し、
負荷分散時は各 listener/プロセスの上限を合算して容量を設計してください。
キュー滞留の保護には [キュー容量制限](#キュー容量とバックプレッシャー) を併用します。

## 起動前の設定検証

```bash
go run ./cmd/kuroshio -check-config -config ./config.yaml
```

成功時は `configuration OK` を標準出力に出して終了コード0で終了します。
不正な設定は値や secret を含まない診断を標準エラーへ出し、終了コード1で終了します。
引数エラーは2です。`go run` 経由では Go 自体の終了ステータス表示も追加されます。
ファイル選択は通常起動と同じく `-config` → `MTA_CONFIG_FILE` → カレントの
`config.yaml` / `config.yml`、値は環境変数 → YAML → 既定値です。
カレント相対の鍵・token パスも通常起動と同じ意味になります。

検証モードは YAML の未知項目（ネスト内を含む）、型・構文の不正、複数文書、
不正な列挙値・環境変数の数値/boolean/duration、負の資源値、主要タイムアウト・worker数、
listener の host:port と同一文字列の重複、容量制限と backend の組合せ、relay/S3設定の必須項目、
TLS鍵ペア、DKIM署名鍵、受信レートルール、Submission認証設定、secret file の読込を検証します。
YAML の誤りは環境変数で上書きされる項目も検出します。既存の通常起動では、未知項目の無視や
一部の不正な環境変数/enum の fallback を維持しています。検証時はこれらもエラーになります。
YAML・TLS parser が入力値を診断へ含める可能性があるため、読み込み段階のエラーは一般化して表示します。

キューのディレクトリ・ロック、credential DB、spool、listener、OTEL exporter、配送 worker は
作成・起動しません。稼働中プロセスと同じファイルで並行実行できます。
SQLite は DSN の必須指定のみを確認し、DB を開かず、schema・資格情報・到達性を検証しません。
外部接続（DNS/Redis/Kafka/S3/relay/IdP）、ディレクトリへの書込権限、ポートの空き、
異なる表記のアドレス同士の bind 競合は別途確認してください。将来の route 参照や未実装の設定を許可する機能ではありません。
稼働中の更新範囲は [設定の限定的な再読込](#設定の限定的な再読込) を参照してください。

## 設定の限定的な再読込

稼働中の `kuroshio` プロセスへ SIGHUP を送ると、起動前検証と同じ厳格な検証を行い、
`log_level` の変更と、起動時に指定した `dkim_profiles_file` の内容更新を反映します。
署名 profile は鍵・DNS を検証後に一式を切り替え、ロガーには新しい世代を反映します。起動時の世代は1で、
変更成功ごとに1増えます。同じ設定の再読込は世代を増やしません。
ログにはロガーと一緒に公開する `config_generation` が付きます。

```bash
# 設定を一時ファイルに書き、検証後に rename で配置することを推奨します。
kuroshio -check-config -config /etc/kuroshio/config.yaml
kill -HUP <kuroshioのPID>
```

listener、認証、秘密情報、TLS/DKIM鍵のパス、キュー、配送、rate limit、受信資源上限など、
`log_level` 以外の YAML 設定値の変更は再起動が必要です。
例外は同じ `dkim_profiles_file` 内の profile 内容更新で、参照先 JSON のパス自体は変更できません。同時に変更されていれば更新全体を拒否します。
誤字・不正値・読めない鍵/secret file も更新を拒否し、旧設定・ロガー・世代を維持します。
再読込時のファイル選択・環境変数優先順位は起動時と同じです。環境変数はプロセス起動時の値なので、
環境変数で指定したログレベルを YAML の変更で上書きすることはできません。
通常起動で許容している未知項目や fallback 対象の不正値も、再読込時は検証エラーになります。

SMTP セッションや配送 worker の起動設定は変更しません。
署名 profile は各署名処理が取得した一組を使い切るため、切替前の進行中処理は旧 selector を使う場合があります。
ログのレベルと世代は1つのロガーとして公開し、並行して記録されるログも各ロガーの一貫した世代を持ちます。
切替前に取得済みのロガーを使う記録は旧世代で出る場合があります。
既存の証明書・従来の単一署名鍵の同じパスでの更新処理は、それぞれの従来の仕組みに従います。
profile モードの鍵は反映時点の一組を保持し、ファイル上書きによる自動更新は行いません。

監視には `config_reload_success_total`、`config_reload_failure_total`、`config_generation` を使います。
成功（変更なしも含む）は `config_reloaded`、失敗は `config_reload_rejected` のログイベントに記録します。
成功イベントは info レベルなので、反映後のログレベルによっては抑止されます。メトリクスでも確認してください。
検証に失敗したら設定を修正して再検証・SIGHUP を行い、再起動必須項目を変える場合は計画的に再起動してください。
SIGHUP は設定更新の完了後に送ってください。短時間に連続した signal はまとめられる場合があります。

再読込のプロセス結合テストは CI でも実行します。ローカルでは以下で確認できます。

```bash
go test -race -tags=integration ./cmd/kuroshio -run '^TestConfigReloadProcess$' -count=1 -v
```

## Policy quarantine の運用

`quarantine.enabled` は既定 `false` で、認証の quarantine は従来どおりヘッダ通知のみです。
次の設定で認証判定と DATA フックの quarantine を永続保留へ接続します。
YAML のみで指定し、反映には再起動が必要です。`retention` は有効時に必須の正の duration（最大8760h）です。

```yaml
queue_backend: local
quarantine:
  enabled: true
  retention: 168h
```

Kafka は隔離管理の保存・共有方式が未実装のため、この組み合わせを起動前検証と起動時に拒否します。
機能を後で無効にしても保存済みの隔離は解除しません。旧 JSON と外部からの X ヘッダは隔離状態を変更しません。
理由は `auth_policy` または `policy_hook`、期限は受信時刻＋retention として保存します。
隔離中は配送 attempt を増やさず、worker の scan 時に期限切れを削除します。
停止中は削除せず、再起動後の scan で処理します。期限切れの手動解放は409で拒否します。
削除による DSN は送らないため、管理者が期限内に確認してください。
認証判定からの DMARC 通知の扱いは従来どおりで、元メールの隔離とは独立しています。

管理 API を有効化した環境で次を使います。認証・actor の監査は既存管理 API と同じです。

| 操作 | HTTP | 必要な role |
| --- | --- | --- |
| 隔離一覧（本文なし） | `GET /api/v1/queue/quarantine?limit=50` | viewer |
| 明示解放 | `POST /api/v1/queue/quarantine/{id}/requeue` | operator |
| 明示削除 | `DELETE /api/v1/queue/quarantine/{id}` | operator |

解放・削除は `?dry_run=1` で変更せず確認できます。解放の dry-run は操作予定の監査だけで、
存在・期限の検証は実操作時に行います。削除の dry-run は存在・隔離状態も確認します。
解放後の再解放は409、存在しない ID は404です。通常キューの requeue を使った解除はできません。
一覧・解放の応答には本文を含めません。本文確認はキューファイルへのアクセス権を持つ管理者が
隔離した環境で行い、API の viewer/operator token に本文参照を許可しません。
隔離ファイルも本文を含むため、queue ディレクトリとバックアップの OS 権限・暗号化を管理してください。

`queue_quarantine_messages`（scan 時更新）と `queue_quarantine_expired_total` を監視します。
保存データの読み取り・削除エラーは worker の queue scan エラーとして確認してください。
件数が増える場合は判定元の障害・誤判定を確認し、解放前に宛先と期限を確認してください。
隔離中も `queue_max_messages` / `queue_max_bytes` / `queue_min_free_bytes` に算入するため、
容量に余裕がなくなると新規受信を451にします。隔離解除は追加容量を要求しません。
期限切れ削除は `queue_quarantine_expired` イベントとして system actor で監査し、本文はログへ出しません。

## 宛先別配送の期限付き停止

`delivery_pause.enabled`（既定 `false`）を有効にすると、管理者が宛先ドメインを
理由・期限付きで停止できます。受信を拒否せず、新しい配送 attempt を保留します。
設定は YAML のみで、反映には再起動が必要です。

```yaml
delivery_pause:
  enabled: true
  backend: local
```

`backend` は `local`（省略時も同じ）または `redis` です。
local は `queue_dir/delivery-pauses.json` に0600のファイルで保存し、再起動後に復元します。
1つのプロセスが専有するファイルです。複数プロセスや複数ノードで共有しないでください。
Redis は既存の `domain_throttle_redis_addrs` / `username` / `password` / `db` / `key_prefix`
設定を使い、domain throttle / MX shaping とは別の `{delivery-pauses}` 名前空間へ保存します。
複数ノードでは全ノードの設定・prefix を揃え、Redis の永続化・バックアップ・HA を運用側で設定します。
起動時の接続失敗で local へ自動切替はしません。Redis 時刻を有効期限判定に使い、
停止情報の障害・読み取り失敗時は新しい配送を保留します。
local/Kafka の worker で共通に照合します。Kafka を複数ノードで使う場合は Redis を選んでください。

停止対象は正確に一致する ASCII ドメインだけです。小文字化と末尾の `.` 除去を行い、
ワイルドカード、サブドメインの暗黙一致、メールアドレスの指定は受け付けません。
同時に最大1024ドメインを保存し、理由は非空・512 bytes以内・制御文字なし、
有効期限は現在より後かつ30日以内です。同じドメインへの停止操作は理由・期限を置き換えます。
作成主体は認証 principal から記録し、任意の actor header を保存主体として使いません。
停止・再開は監査ログに reason と期限（停止時）、認証主体を記録します。

| 操作 | HTTP | 必要な role |
| --- | --- | --- |
| 有効な停止一覧 | `GET /api/v1/delivery/pauses` | viewer |
| 停止・期限更新 | `POST /api/v1/delivery/pauses` | operator |
| 明示再開 | `DELETE /api/v1/delivery/pauses/{domain}?reason=restored` | operator |

停止の JSON は `domain`、`reason`、RFC3339 の `expires_at`、任意の `dry_run` を指定します。
再開の `reason` も必須です。再開では `?dry_run=1&reason=restored` が使えます。
どちらの dry-run も入力・容量を検証し、保存内容を変えず監査を記録します。
期限切れは照合時から無効となり、次回の新規停止保存時に古い情報を掃除します。

停止前にチェックを通過した SMTP 処理は完了させます。ドメインスロット待ちの後にも
照合し、まだ配送を始めていない処理には新しい停止を反映します。
停止中は `Defer` で最大30秒後（停止期限が近ければその時刻）に再確認し、
attempt と LastError を増やさず、遅延 DSN も生成しません。
同じメールの別ドメインは配送を継続し、配送済み recipient を再送対象から外します。
別 recipient の実際の配送失敗がある場合は、その失敗による既存の再送回数を共有します。
再開は手動操作・期限切れのいずれも次回確認時に反映し、既存の流量制御・Warm-up を通します。

保留も `max_retry_age` の経過に含めます。停止照合時にメッセージ寿命へ達していた recipient は
配送を打ち切り、既存のループ防止条件に従う失敗 DSN を生成します。
`max_retry_age: 0` は既存どおり寿命上限なしです。
停止機能を無効化すると照合も停止するため、未解除の停止を維持する場合は有効なまま運用してください。

`delivery_pause_active_domains`、`worker_pause_deferred_total`、`worker_pause_backend_error_total`、
`worker_pause_expired_total` を監視します。ローカルキューでは scan 時に
`delivery_pause_messages`（メッセージ単位、隔離を除く）と
`delivery_pause_oldest_age_seconds`（受信からの経過時間）も全 pending ファイルから計算します。
Kafka のドメイン別全滞留統計はこの段階では未提供であり、broker lag と延期カウンターを併用してください。
