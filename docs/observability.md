# Observability

`kuroshio-mta` の observability は、`Prometheus` 形式の metrics、`/slo` の JSON レポート、`slog` による JSON ログを中心に構成しています。加えて、現在は OpenTelemetry tracing を OTLP/HTTP exporter 経由で有効化できます。

このページでは、今ある観測手段と OpenTelemetry (`OTEL`) との関係を整理します。

## 現在あるもの

### `/healthz`

- endpoint: `GET /healthz`
- 返り値: `200 OK` と `ok`

最小の liveness 確認向けです。

### `/metrics`

- endpoint: `GET /metrics`
- 形式: Prometheus text exposition

`observability_addr` で待ち受けている HTTP サーバから公開されます。
設定は [Configuration](/configuration) の `observability_addr` を参照してください。

コード上では、内部カウンタを集めて Prometheus 形式へレンダリングしています。

### `/slo`

- endpoint: `GET /slo`
- 形式: JSON
- 正常時: `200 OK`
- breach 時: `503 Service Unavailable`

`/slo` は metrics の snapshot から、配送成功率・retry 率・queue backlog を評価して返します。
しきい値は次の環境変数で上書きできます。

- `MTA_SLO_MIN_DELIVERY_SUCCESS_RATE`
- `MTA_SLO_MAX_RETRY_RATE`
- `MTA_SLO_MAX_QUEUE_BACKLOG`

### `/reputation`

- endpoint: `GET /reputation`
- 形式: JSON

reputation tracker を有効にしている場合だけ値が返ります。

### JSON ログ

アプリケーションログは `slog` ベースの JSON です。
`log_level` で `debug` / `info` / `warn` / `error` を切り替えます。

## 現在の OTEL 対応状況

現時点の `kuroshio-mta` では、OpenTelemetry SDK を使った trace export を有効化できます。

- exporter: OTLP/HTTP
- 有効化: `otel_enabled: true`
- 送信先: `otel_exporter_otlp_endpoint`
- sampling: `otel_trace_sample_ratio`
- 現在 span を付与している箇所:
  - SMTP session
  - worker の message 処理

現状の observability を一言で言うと:

- metrics: 独自カウンタを Prometheus 形式で公開
- SLO: `/slo` で独自評価
- logs: `slog` JSON
- tracing: OpenTelemetry + OTLP/HTTP

## OTEL という言葉をどう読むべきか

`kuroshio-mta` の docs やコード上で `trace` という語が出ることはありますが、
これはメールの `Received:` や trace header 文脈の話で、OpenTelemetry trace を意味しているわけではありません。

OTEL 観点で見ると、将来的には次のような分離が自然です。

- logs: `slog` のまま、必要なら Collector 経由で集約
- metrics: Prometheus scrape を維持するか、OTEL metrics exporter を追加
- traces: SMTP 受信、queue、worker、delivery を span 化

まだ入っていないものは次です。

- OpenTelemetry Meter を使った metrics export
- OpenTelemetry Log SDK 連携
- trace と log の相関 ID 自動付与
- Collector 前提の deployment サンプル

ただし、これは今後の拡張方針であって、現実装の説明ではありません。

## 最初に確認するコマンド

ローカル起動済みなら、まずは次で十分です。

```bash
curl http://127.0.0.1:9090/healthz
curl http://127.0.0.1:9090/metrics | head
curl http://127.0.0.1:9090/slo
```

tutorial から試すなら次を入口にしてください。

- [最小メールフローを試す](/tutorials/basic-mail-flow)
- [Rate Limit を試す](/tutorials/rate-limit)
- [Admin API を試す](/tutorials/admin-operations)

## 関連ドキュメント

- [Configuration](/configuration)
- [SLO Delivery](/runbooks/slo_delivery)
- [SLO Retry](/runbooks/slo_retry)
- [HA Reference](/architecture/ha_reference)
