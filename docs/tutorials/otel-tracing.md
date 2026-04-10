# OTEL Tracing を試す

`kuroshio-mta` の OpenTelemetry tracing を、`OTLP/HTTP -> OpenTelemetry Collector -> Jaeger` の最小構成で確認する tutorial です。

この tutorial では、SMTP で 1 通受けたときの trace を Collector 経由で Jaeger に送り、UI と API の両方から確認します。

## 前提

- Docker と `docker compose` が使えること
- ローカルで `:2525`、`:9090`、`:16686`、`:4318` を使えること

使う compose 一式は [examples/tutorials/otel-tracing](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/otel-tracing) にあります。

## 起動するもの

- `kuroshio`: tutorial 用の MTA 本体
- `smtp-client`: SMTP セッション投入用の補助コンテナ
- `otel-collector`: OTLP/HTTP receiver と Jaeger 向け exporter
- `jaeger`: trace 可視化用 UI

## 1. compose を起動する

```bash
docker compose -f examples/tutorials/otel-tracing/compose.yaml up --build -d
```

MTA は `otel_enabled: true` で起動し、`otel_exporter_otlp_endpoint` として `http://otel-collector:4318/v1/traces` を使います。

## 2. SMTP で 1 通投入する

```bash
docker compose -f examples/tutorials/otel-tracing/compose.yaml exec smtp-client sh -lc '
cat <<EOF | nc kuroshio 2525
EHLO tutorial.local
MAIL FROM:<sender@example.com>
RCPT TO:<recipient@example.net>
DATA
Subject: OTEL tutorial

trace me
.
QUIT
EOF
'
```

この 1 通で、少なくとも次の span 群が発生します。

- SMTP session
- queue の enqueue / due / ack
- worker の message 処理

`delivery_mode: local_spool` にしているので、外部配送先は不要です。

## 3. Jaeger UI で trace を見る

ブラウザで `http://127.0.0.1:16686` を開き、`Service` に `kuroshio-mta` を選びます。

最新 trace がまだ見えないときは、`Lookback` を広げて `Find Traces` を押してください。

CLI から確認するなら次でも service 一覧を見られます。

```bash
curl http://127.0.0.1:16686/api/services
```

## 4. Collector 側で受信も確認する

Collector の `debug` exporter を有効にしているので、trace を受け取るとログにも span が出ます。

```bash
docker compose -f examples/tutorials/otel-tracing/compose.yaml logs otel-collector
```

## 5. MTA の metrics と spool も見る

trace とは別に、既存の observability も並行して確認できます。

```bash
curl http://127.0.0.1:9090/metrics | head
ls examples/tutorials/otel-tracing/var/spool
```

## 6. 後片付け

```bash
docker compose -f examples/tutorials/otel-tracing/compose.yaml down
```

queue と spool も消したい場合だけ次を実行します。

```bash
rm -rf examples/tutorials/otel-tracing/var
```

## 関連

- [Observability](/observability)
- [最小メールフローを試す](/tutorials/basic-mail-flow)
- [Tutorials Home](/tutorials/)
