# Grafana で trace と log を紐づける

`kuroshio-mta` の JSON ログには、OpenTelemetry tracing を有効にすると `trace_id` と `span_id` が入ります。

この tutorial では、[OTEL Tracing を試す](/tutorials/otel-tracing) と同じ compose 環境に `Loki` と `Promtail` を追加し、Grafana 上で trace と log を相互に辿れるようにします。

## 前提

- Docker と `docker compose` が使えること
- ローカルで `:3000`、`:3100`、`:3200`、`:4318` を使えること
- 先に [OTEL Tracing を試す](/tutorials/otel-tracing) を一度読んでおくと流れを追いやすいです

使う compose 一式は [examples/tutorials/otel-tracing](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/otel-tracing) にあります。

## 起動するもの

- `kuroshio`: JSON ログと trace を出す MTA 本体
- `otel-collector`: trace を Tempo に送る Collector
- `tempo`: trace 保存先
- `loki`: log 保存先
- `promtail`: Docker logs を Loki に送る agent
- `grafana`: trace / log の UI

## 1. compose を起動する

```bash
docker compose -f examples/tutorials/otel-tracing/compose.yaml up --build -d
```

この compose では Grafana datasource を provisioning 済みです。

- `Tempo`: trace 用
- `Loki`: log 用

加えて、次の相関設定も入っています。

- `Tempo -> Loki`: trace から同じ `trace_id` のログを検索
- `Loki -> Tempo`: ログ中の `trace_id` から trace を開く

## 2. trace と log を発生させる

```bash
docker compose -f examples/tutorials/otel-tracing/compose.yaml exec smtp-client sh -lc '
cat <<EOF | nc kuroshio 2525
EHLO tutorial.local
MAIL FROM:<sender@example.com>
RCPT TO:<recipient@example.net>
DATA
Subject: trace-log tutorial

link trace and logs
.
QUIT
EOF
'
```

## 3. Loki にログが入っているか確認する

まず Loki の readiness を確認します。

```bash
curl http://127.0.0.1:3100/ready
```

Promtail が `kuroshio` コンテナを拾えているかは次で見られます。

```bash
docker compose -f examples/tutorials/otel-tracing/compose.yaml logs promtail
```

## 4. Grafana Explore でログを見る

ブラウザで `http://127.0.0.1:3000` を開き、`Explore` で datasource に `Loki` を選びます。

次のような query で `kuroshio` のログを絞れます。

```text
{compose_service="kuroshio"}
```

ログ詳細を見ると、JSON フィールドとして `trace_id` と `span_id` が含まれます。

## 5. log から trace を開く

Loki datasource には derived field を設定してあるので、`trace_id` を含むログ行から `Tempo` の trace へ直接移動できます。

Grafana のログ詳細で `TraceID` リンクが見えたら、それを開いて trace を確認してください。

## 6. trace から対応ログを開く

今度は datasource を `Tempo` に切り替えて、`Service Name = kuroshio-mta` で trace を検索します。

trace 詳細画面では `Logs for this span` 相当のリンクから、同じ `trace_id` を持つ Loki ログへ移動できます。

## 7. 何が紐づいているのか

この tutorial の相関は、アプリ側で埋め込まれた `trace_id` / `span_id` に依存しています。

実装上は [internal/logging/otel_handler.go](https://github.com/tamago0224/kuroshio-mta/blob/main/internal/logging/otel_handler.go) で、`InfoContext` / `WarnContext` / `ErrorContext` 経由の JSON ログに `trace_id` と `span_id` を追加しています。

## 8. 後片付け

```bash
docker compose -f examples/tutorials/otel-tracing/compose.yaml down
```

生成された queue / spool を消したい場合だけ次を実行します。

```bash
rm -rf examples/tutorials/otel-tracing/var
```

## 関連

- [OTEL Tracing を試す](/tutorials/otel-tracing)
- [Observability](/observability)
- [Tutorials Home](/tutorials/)
