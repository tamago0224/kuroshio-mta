# Tutorials

`kuroshio-mta` の主要機能を、手元で実際に動かしながら確認するための入口です。

ほとんどの tutorial は `docker compose` で最小環境を起動し、SMTP・Admin API・DNS・MTA-STS policy などをそのまま確認できるようにしています。

## まずどこから試すか

| Tutorial | 何を確認するか | 主な環境 |
| --- | --- | --- |
| [最小メールフローを試す](/tutorials/basic-mail-flow) | SMTP で 1 通受信し、local queue に入るところ | `kuroshio` + `smtp-client` |
| [OTEL Tracing を試す](/tutorials/otel-tracing) | OTLP/HTTP で Alloy に trace を送り、Grafana で見る | `kuroshio` + `alloy` + `tempo` + `grafana` |
| [Grafana で trace と log を紐づける](/tutorials/otel-logs-loki) | `trace_id` / `span_id` を使って Tempo と Loki を往復する | `kuroshio` + `alloy` + `tempo` + `loki` + `grafana` |
| [S3 Spool Backend を観測する](/tutorials/s3-spool-observability) | `spool_backend: s3` で MinIO に `.eml` が保存される流れ | `kuroshio` + `minio` + `smtp-client` |
| [Kafka Queue Mode を観測する](/tutorials/kafka-queue-observability) | `queue_backend: kafka` で topic と worker 処理を追う | `kuroshio` + `kafka` + `smtp-client` |
| [Rate Limit を試す](/tutorials/rate-limit) | 接続元 IP や `MAIL FROM` 単位の制限 | `kuroshio` + `smtp-client` |
| [Domain Throttle を観測する](/tutorials/domain-throttle-observability) | Redis backend で配送側 throttle の wait と fail-open を見る | `kuroshio` + `redis` + `slow-smtp` |
| [Admin API を試す](/tutorials/admin-operations) | queue 操作と Admin API / CLI | `kuroshio` + `smtp-client` |
| [Submission Auth を試す](/tutorials/submission-auth) | `AUTH PLAIN` / `AUTH LOGIN` と sender scope | `kuroshio` + `smtp-client` |
| [メール認証を試す](/tutorials/mail-auth) | SPF / DMARC 評価、DKIM / ARC 署名 | `CoreDNS` + `policy` + `tester` |
| [TLS 配送ポリシーを試す](/tutorials/tls-policy) | STARTTLS、MTA-STS、DANE | `CoreDNS` + `policy` + `tester` |

## compose ベースの tutorial 環境

- 基本 SMTP: [examples/tutorials/basic-mail-flow](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/basic-mail-flow)
- OTEL tracing: [examples/tutorials/otel-tracing](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/otel-tracing)
- S3 spool observability: [examples/tutorials/s3-spool-observability](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/s3-spool-observability)
- Kafka queue observability: [examples/tutorials/kafka-queue-observability](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/kafka-queue-observability)
- Rate Limit: [examples/tutorials/rate-limit](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/rate-limit)
- Domain throttle observability: [examples/tutorials/domain-throttle-observability](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/domain-throttle-observability)
- Admin API: [examples/tutorials/admin-operations](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/admin-operations)
- Submission Auth: [examples/tutorials/submission-auth](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/submission-auth)
- DNS / Web: [examples/tutorials/dns-services](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/dns-services)

## 進み方のおすすめ

1. [Getting Started](/getting-started) で最初の起動方法を確認する
2. 先に [Observability Stack](/observability_stack) で Alloy / Tempo / Loki / Grafana の役割をつかむ
3. [最小メールフローを試す](/tutorials/basic-mail-flow) で 1 通受ける
4. [OTEL Tracing を試す](/tutorials/otel-tracing) で trace の見え方を確認する
5. [Grafana で trace と log を紐づける](/tutorials/otel-logs-loki) で trace とログの相関を見る
6. [S3 Spool Backend を観測する](/tutorials/s3-spool-observability) と [Kafka Queue Mode を観測する](/tutorials/kafka-queue-observability) で backend 差し替え時の見え方を確認する
7. [Rate Limit を試す](/tutorials/rate-limit) と [Domain Throttle を観測する](/tutorials/domain-throttle-observability) と [Admin API を試す](/tutorials/admin-operations) で運用系を見る
8. [メール認証を試す](/tutorials/mail-auth) と [TLS 配送ポリシーを試す](/tutorials/tls-policy) で DNS / policy 系を確認する
