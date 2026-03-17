# Reputation Operations Runbook

Issue: #37

## 目的

- IP / ドメインレピュテーション低下時に自動で送信制御する
- warm-up、bounce rate、complaint rate、TLS-RPT を可視化する

## 有効化

- `MTA_REPUTATION_START_DATE=2026-03-17`
- `MTA_REPUTATION_WARMUP_RULES=0:100,7:1000,14:5000`
- `MTA_REPUTATION_BOUNCE_THRESHOLD=0.05`
- `MTA_REPUTATION_COMPLAINT_THRESHOLD=0.001`
- `MTA_REPUTATION_MIN_SAMPLES=100`

## 動作

- warm-up 制限を超えると、その日の追加送信は一時抑止される
- permanent failure 比率が閾値以上になると送信を一時抑止する
- complaint 比率が閾値以上になると送信を一時抑止する
- TLS-RPT / complaint は管理API経由で記録できる

## 可視化

- `/reputation` でドメインごとの状態を JSON 取得できる
- メトリクス:
  - `worker_reputation_block_total`
  - `reputation_warmup_limit_total`
  - `reputation_bounce_rate_block_total`
  - `reputation_complaint_rate_block_total`

## 収集操作

```bash
scripts/admin/orinoco_admin.sh record-complaint gmail.com
scripts/admin/orinoco_admin.sh record-tlsrpt gmail.com false
```

## 判定の見方

- `blocked=true`:
  そのドメイン向け配送は一時抑止中
- `block_reason=warmup_limit`:
  warm-up 日次上限に到達
- `block_reason=bounce_rate`:
  permanent failure 比率が閾値超過
- `block_reason=complaint_rate`:
  complaint 比率が閾値超過

## 制約

- complaint / TLS-RPT の自動受信パーサは未実装
- 現時点では API からの記録と in-memory 集計が中心
