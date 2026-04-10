# Kafka Queue モード

Kafka Queue モードは `config.yaml` の `queue_backend: kafka` を主として管理し、必要な差分だけ環境変数で上書きします。

## 対応する設定項目

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `queue_backend` | `MTA_QUEUE_BACKEND` | `local` | キューバックエンドを `kafka` に切り替えます |
| `kafka_brokers` | `MTA_KAFKA_BROKERS` | `localhost:9092` | Kafka broker 一覧です |
| `kafka_consumer_group` | `MTA_KAFKA_CONSUMER_GROUP` | `kuroshio-mta` | Kafka consumer group 名です |
| `kafka_topic_inbound` | `MTA_KAFKA_TOPIC_INBOUND` | `mail.inbound` | inbound メッセージの topic 名です |
| `kafka_topic_retry` | `MTA_KAFKA_TOPIC_RETRY` | `mail.retry` | retry メッセージの topic 名です |
| `kafka_topic_dlq` | `MTA_KAFKA_TOPIC_DLQ` | `mail.dlq` | dead-letter queue の topic 名です |
| `kafka_topic_sent` | `MTA_KAFKA_TOPIC_SENT` | `mail.sent` | 送信完了メッセージの topic 名です |

## YAML 例

```yaml
queue_backend: kafka

kafka_brokers:
  - localhost:9092
kafka_consumer_group: kuroshio-mta
kafka_topic_inbound: mail.inbound
kafka_topic_retry: mail.retry
kafka_topic_dlq: mail.dlq
kafka_topic_sent: mail.sent
```

環境差分を出したい場合は、broker だけ環境変数で差し替える運用もできます。

## 環境変数で上書きする例

```bash
MTA_QUEUE_BACKEND="kafka" \
MTA_KAFKA_BROKERS="localhost:9092" \
MTA_KAFKA_CONSUMER_GROUP="kuroshio-mta" \
MTA_KAFKA_TOPIC_INBOUND="mail.inbound" \
MTA_KAFKA_TOPIC_RETRY="mail.retry" \
MTA_KAFKA_TOPIC_DLQ="mail.dlq" \
MTA_KAFKA_TOPIC_SENT="mail.sent" \
go run ./cmd/kuroshio
```

## ローカル起動例

```bash
docker compose -f docker-compose.kafka.yml up -d
```

compose で queue の流れと observability をまとめて確認したい場合は
[Kafka Queue Mode を観測する](/tutorials/kafka-queue-observability) を参照してください。

## 運用メモ

- `kafka_brokers` は YAML では配列、環境変数ではカンマ区切りで指定します
- まず YAML に topic 名や consumer group を固定し、環境ごとに変わる broker だけ上書きすると管理しやすくなります
- 他の設定項目も含めた全体一覧は [configuration.md](./configuration.md) を参照してください
