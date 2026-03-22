# Load / Chaos Test Runbook

Issue: #35

## 目的

- SMTP受信のスループットと遅延を定量化する
- 障害注入時の劣化挙動を再現し、運用上のボトルネックを特定する

## シナリオ

- normal: 日常想定
- peak: ピーク想定
- degraded: 障害時想定（HAドリル併用）

## 目標値

- normal: `tps >= 40`, `p95_ms <= 100`, `failed == 0`
- peak: `tps >= 80`, `p95_ms <= 250`, `failed / requested <= 0.01`
- degraded: `tps >= 20`, `p95_ms <= 300`, `failed / requested <= 0.05`

## 実行手順

1. MTAを起動

```bash
go run ./cmd/kuroshio
```

2. 単体シナリオ実行

```bash
scripts/load/run_smtp_load.sh normal 127.0.0.1:2525
scripts/load/run_smtp_load.sh peak 127.0.0.1:2525
scripts/load/run_smtp_load.sh degraded 127.0.0.1:2525
```

3. カオス併用スイート実行

```bash
scripts/chaos/run_load_chaos_suite.sh 127.0.0.1:2525 --apply ./var/load-chaos/results.ndjson
```

## 出力

`cmd/smtpload` は JSON を出力する。

```json
{"address":"127.0.0.1:2525","concurrency":10,"requested":200,"succeeded":200,"failed":0,"duration_sec":4.2,"tps":47.6,"avg_ms":20.1,"p95_ms":35.0,"max_ms":58.3,"started_at_utc":"2026-03-16T00:00:00Z"}
```

## 判定観点

- TPS: 目標TPSを満たすか
- p95遅延: 受け入れ閾値以内か
- 失敗率: `failed / requested` が許容範囲か
- カオス時の復帰: ドリル後に再び normal 相当の指標へ戻るか

## 容量計画の記録

- `scripts/load/run_smtp_load.sh` の第3引数に `results.ndjson` を渡すと、シナリオ付きで結果を追記できる
- `scripts/load/plan_capacity.sh ./var/load-chaos/results.ndjson` で Markdown 表に整形できる
- 週次で同一条件の結果を記録し、CPU/メモリ/ディスク使用率と併せて容量計画表へ転記する

出力例:

```text
| scenario | concurrency | requested | succeeded | failed | tps | avg_ms | p95_ms | max_ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| normal | 10 | 200 | 200 | 0 | 47.6 | 20.1 | 35.0 | 58.3 |
| degraded | 20 | 500 | 497 | 3 | 22.4 | 84.3 | 140.2 | 220.7 |
```

## 次の拡張候補

- Queue backend（Kafka）別のシナリオ追加
- SMTP Submission(587) 経路の負荷シナリオ追加
- 指標収集を Prometheus pushgateway 連携で自動化
