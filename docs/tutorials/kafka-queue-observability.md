# Kafka Queue Mode を観測する

`queue_backend: kafka` で `kuroshio-mta` を動かし、受信したメールが Kafka topic に入り、worker が取り出して処理する流れを確認する tutorial です。

この tutorial では `delivery_mode: local_spool` を組み合わせて、外部配送先を用意せずに

- Kafka topic
- `kuroshio-mta` の metrics
- MTA のログ
- local spool

を見ながら動作確認します。

## 前提

- Docker と `docker compose` が使える
- ローカルで `:2525`、`:9090`、`:9092` を使える

使う compose 一式は [examples/tutorials/kafka-queue-observability](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/kafka-queue-observability) にあります。

## 起動するもの

- `kuroshio`: tutorial 用設定で起動する `kuroshio-mta`
- `smtp-client`: SMTP セッション投入用
- `kafka`: queue backend

## 1. compose を起動する

```bash
docker compose -f examples/tutorials/kafka-queue-observability/compose.yaml up --build -d
```

この tutorial では次の設定を使います。

- `queue_backend: kafka`
- `kafka_topic_inbound: mail.inbound`
- `kafka_topic_retry: mail.retry`
- `kafka_topic_dlq: mail.dlq`
- `kafka_topic_sent: mail.sent`
- `delivery_mode: local_spool`

## 2. SMTP で 1 通投入する

```bash
docker compose -f examples/tutorials/kafka-queue-observability/compose.yaml exec smtp-client sh -lc '
cat <<EOF | nc kuroshio 2525
EHLO tutorial.local
MAIL FROM:<sender@example.com>
RCPT TO:<recipient@example.net>
DATA
Subject: Kafka queue tutorial

hello to kafka
.
QUIT
EOF
'
```

## 3. Kafka topic を確認する

worker が処理する前後で、Kafka topic にメッセージが流れます。

たとえば `mail.sent` を見るなら次を使います。

```bash
docker compose -f examples/tutorials/kafka-queue-observability/compose.yaml exec kafka sh -lc '
/opt/bitnami/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server kafka:9092 \
  --topic mail.sent \
  --from-beginning \
  --max-messages 1
'
```

状況によっては `mail.inbound` や `mail.retry` を見ると流れがわかりやすいです。

## 4. spool と metrics を確認する

```bash
find examples/tutorials/kafka-queue-observability/var/spool -maxdepth 2 -type f | sort
curl http://127.0.0.1:9090/metrics | head
```

この tutorial では `delivery_mode: local_spool` にしているので、最終的に `.eml` は local spool に保存されます。

見る場所の役割は次の通りです。

- Kafka topic:
  queue backend を通っていることの確認
- local spool:
  worker が最終的に処理したことの確認
- metrics:
  MTA の counters の確認
- logs:
  individual failure や retry 理由の確認

## 5. ログを見る

```bash
docker compose -f examples/tutorials/kafka-queue-observability/compose.yaml logs kuroshio
```

## 6. 後片付け

```bash
docker compose -f examples/tutorials/kafka-queue-observability/compose.yaml down
rm -rf examples/tutorials/kafka-queue-observability/var
```

## 関連

- [Kafka Queue モード](/kafka_queue_mode)
- [Observability](/observability)
- [Tutorials Home](/tutorials/)
