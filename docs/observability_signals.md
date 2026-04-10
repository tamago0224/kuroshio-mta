# Observability Signals

このページは、`kuroshio-mta` の observability signal をどう使い分けるか、そして今後どこまで OpenTelemetry に寄せるかを整理するための方針メモです。

対象は次の 3 つです。

- metrics
- logs
- traces

stack の全体像は [Observability Stack](/observability_stack)、
現実装が今出している signal は [Observability](/observability) を参照してください。

## 結論

当面の方針は次です。

- metrics:
  既存の `/metrics` を維持する
- SLO:
  `/slo` の独自評価を維持する
- logs:
  `slog` JSON を維持し、`trace_id` / `span_id` で traces と相関させる
- traces:
  OpenTelemetry を継続して広げる

つまり、短中期では

- metrics は Prometheus 互換を主軸
- logs は `slog` JSON を主軸
- traces は OpenTelemetry を主軸

で進めます。

## なぜこの分担にするか

### metrics

metrics は、いまの `kuroshio-mta` では次の用途に直結しています。

- delivery 成功率
- retry 率
- queue backlog
- rate limit や rejection の件数

これらはすでに `/metrics` と `/slo` で運用に載せやすく、Prometheus 系の扱いに馴染みます。

なので、当面は次を優先します。

- `/metrics` を壊さない
- SLO 判定に必要なカウンタを維持する
- alerting や dashboard の互換性を守る

### logs

logs は、個別の失敗理由や運用イベントを見るための signal です。

特に `kuroshio-mta` では、

- SMTP reject 理由
- queue の操作失敗
- delivery の失敗内容
- suppression や reputation の運用イベント

の確認に向いています。

`trace_id` / `span_id` をログに載せることで、trace から失敗ログへ辿れる形を維持します。

### traces

traces は、1 通のメールがどこを通って失敗・retry・配送成功したかを見る signal です。

`kuroshio-mta` では、特に次の流れの可視化に価値があります。

- SMTP session
- `MAIL` / `RCPT` / `DATA` / `AUTH` / `STARTTLS`
- queue `enqueue` / `due` / `retry` / `fail` / `ack`
- worker の message 処理
- MX / DANE / MTA-STS lookup
- SMTP delivery attempt

この領域は OpenTelemetry を伸ばす価値が大きいです。

## 現時点で維持するもの

当面は次を維持します。

### 維持するもの

- `/metrics`
- `/slo`
- `slog` JSON
- `trace_id` / `span_id` を含むログ相関
- OTLP/HTTP による trace export

### まだ入れないもの

- OpenTelemetry Log SDK への全面移行
- OpenTelemetry Metrics への全面移行
- `/metrics` 廃止
- `/slo` を OTEL backend 前提へ置き換えること

## OpenTelemetry Metrics について

OpenTelemetry Metrics 自体を否定するわけではありません。
ただし、いまの `kuroshio-mta` では次の理由で優先度を下げます。

- `/metrics` と `/slo` がすでに運用に乗せやすい
- metrics の二重出力は運用判断を複雑にしやすい
- traces 拡張の方が、現時点では価値が高い

なので、順番としては次を推奨します。

1. traces を先に広げる
2. log 相関を安定させる
3. その後で OTEL metrics の要否を判断する

## privacy と属性設計

observability signal には個人情報や配送情報が混ざりやすいため、次の方針を維持します。

- logs では必要に応じてメールアドレスを mask する
- traces でも raw address をむやみに増やさない
- trace attribute には理由や件数を優先し、本文は載せない
- metrics には個別メッセージ情報を載せない

つまり、

- metrics は集計値
- logs は運用詳細
- traces はフローの因果

という役割に合わせて、出す情報の粒度も分けます。

## signal ごとの役割分担

| signal | 主な用途 | 例 |
| --- | --- | --- |
| metrics | 数を継続監視する | success rate、retry rate、backlog |
| logs | 個別の事象を読む | reject reason、enqueue failure、suppression add failure |
| traces | フロー全体を追う | SMTP session から queue、worker、delivery まで |

## 実運用での読み方

おすすめの見方は次です。

1. まず `/slo` や dashboard で異常傾向を見る
2. metrics でどの系統が崩れているかを見る
3. traces でどこで詰まっているか追う
4. logs で失敗理由の詳細を確認する

この順番にすると、数字、流れ、詳細の 3 層で無理なく切り分けられます。

## 今後の候補

将来的に検討する候補は次です。

- OTEL metrics の限定導入
- Alloy への metrics pipeline 集約
- より細かい trace attribute と event の整理
- runbook から trace / log の調べ方への導線追加

ただし、このページの結論は変わりません。

- metrics は当面 Prometheus 互換維持
- logs は `slog` JSON 維持
- traces は OpenTelemetry を主軸に拡張
