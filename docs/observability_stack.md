# Observability Stack

`kuroshio-mta` の observability は、1 つの製品や 1 つの protocol だけで完結しているわけではありません。

いまの構成は大きく分けると次の 3 層です。

- `kuroshio-mta` 自身が出す signal
- signal を受け取って転送する collector / agent
- signal を保存して可視化する backend / UI

このページでは、`Alloy`、`Tempo`、`Loki`、`Grafana`、そして `kuroshio-mta` 自身の役割をまとめます。

## 全体像

最小構成のイメージは次です。

```text
kuroshio-mta
  |- /metrics
  |- slog JSON logs
  `- OTLP/HTTP traces

           |
           v
        Alloy
      /       \
     v         v
  Tempo      Loki
     \         /
      \       /
       v     v
        Grafana
```

signal ごとに言い換えると次の通りです。

- metrics:
  `kuroshio-mta` が `/metrics` で公開し、Prometheus 互換の取り方で収集する
- logs:
  `kuroshio-mta` が `slog` JSON を出し、`trace_id` / `span_id` を含めて Loki へ送る
- traces:
  `kuroshio-mta` が OTLP/HTTP で span を出し、Alloy 経由で Tempo へ送る

## 各コンポーネントの役割

### `kuroshio-mta`

観測対象そのものです。

いまの実装で出しているもの:

- `/healthz`
- `/metrics`
- `/slo`
- `/reputation`
- `slog` JSON logs
- OpenTelemetry traces

trace では、少なくとも次の流れを追えます。

- SMTP session
- `MAIL` / `RCPT` / `DATA` / `AUTH` / `STARTTLS`
- queue の `enqueue` / `due` / `ack` / `retry` / `fail`
- worker の message 処理
- delivery の MX / DANE / MTA-STS / SMTP 試行

実装側の signal については [Observability](/observability) を参照してください。

### `Alloy`

`Alloy` は collector / agent の役目です。

この repo の tutorial では、次を Alloy にまとめています。

- OTLP/HTTP で trace を受ける
- `kuroshio-mta` のログファイルを読む
- Tempo へ trace を転送する
- Loki へログを転送する

つまり `Alloy` 自体は可視化 UI ではなく、signal の受け口と中継点です。

### `Tempo`

`Tempo` は trace backend です。

ここに保存された span を、Grafana の Explore から確認します。
SMTP セッション、queue、worker、delivery の span は Tempo 側でまとまって見えます。

### `Loki`

`Loki` はログ backend です。

`kuroshio-mta` の JSON ログを保存し、`trace_id` / `span_id` を使って trace と相互参照できるようにします。

### `Grafana`

`Grafana` は UI です。

この tutorial では主に次を行います。

- Tempo の trace を見る
- Loki のログを検索する
- trace からログへ飛ぶ
- ログの `trace_id` から trace へ戻る

## 今の `kuroshio-mta` で何をどこで見るか

| 見たいもの | 主な場所 | 役割 |
| --- | --- | --- |
| alive / health | `/healthz` | 最小の生存確認 |
| counters / rates | `/metrics` | Prometheus 互換 metrics |
| SLO 判定 | `/slo` | backlog / delivery / retry の簡易評価 |
| 構造化ログ | stdout / file -> Loki | 運用ログとエラー確認 |
| request / flow trace | OTLP/HTTP -> Tempo | どこで reject / retry / fail したか |

つまり:

- まず数字を見るなら `/metrics`
- アラート判断の近道は `/slo`
- 個別の失敗原因は logs
- 流れ全体は traces

という読み分けです。

## Tutorials との関係

この guide は「全体像」を理解するページです。
実際に手元で触る手順は tutorial 側にあります。

- trace の基本: [OTEL Tracing を試す](/tutorials/otel-tracing)
- trace とログの相関: [Grafana で trace と log を紐づける](/tutorials/otel-logs-loki)
- 基本の SMTP フロー: [最小メールフローを試す](/tutorials/basic-mail-flow)

## ローカル tutorial 構成と本番寄り構成

local tutorial では、できるだけ理解しやすい最小構成を使っています。

- `kuroshio-mta`
- `Alloy`
- `Tempo`
- `Loki`
- `Grafana`

本番寄りの構成では、次の違いが出ます。

- Grafana / Tempo / Loki を外部サービス化する
- Alloy を複数ノードに分散する
- `/metrics` は Prometheus に scrape させる
- retention、auth、network policy を別途設計する

ただし、signal の役割分担そのものは大きく変わりません。

## まず読む順番

1. `kuroshio-mta` 自身の signal を知る: [Observability](/observability)
2. stack 全体の役割をつかむ: このページ
3. 実際に trace を見る: [OTEL Tracing を試す](/tutorials/otel-tracing)
4. ログ相関を見る: [Grafana で trace と log を紐づける](/tutorials/otel-logs-loki)
