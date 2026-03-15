# DR Backup / Restore Runbook

Issue: #34

## 目的

- 災害対策（Disaster Recovery, DR）時に、Orinoco MTA のキュー状態を短時間で復旧する
- 目標値を明示して定期的に検証する
  - RTO (Recovery Time Objective): 5分以内
  - RPO (Recovery Point Objective): 1分以内

## 対象データ

- `MTA_QUEUE_DIR` 配下（既定: `./var/queue`）
  - `mail.inbound`
  - `mail.retry`
  - `mail.dlq`
  - `sent`
  - `suppression.json`

## 事前準備

- バックアップ保存先を用意（例: `./var/backups`）
- バックアップ周期を RPO 目標以下に設定（例: 1分ごと）
- 復旧手順を本番相当環境で定期演習

## バックアップ手順

```bash
scripts/dr/backup_queue.sh ./var/queue ./var/backups
```

出力例:

```text
backup_created=./var/backups/orinoco-queue-20260316T010101Z.tar.gz host=mta-a queue_dir=./var/queue
```

## リストア手順

```bash
scripts/dr/restore_queue.sh ./var/backups/orinoco-queue-20260316T010101Z.tar.gz ./var/queue --force
```

- `--force` なしの場合、復旧先が空でないと失敗する
- `--force` ありの場合、復旧先の既存データを削除して展開する

## DRドリル手順

```bash
scripts/chaos/run_dr_drill.sh ./var/queue ./var/backups --apply
```

このスクリプトは以下を実行する:

1. バックアップ作成
2. 障害注入を想定した停止フェーズ（オペレーター作業）
3. 最新アーカイブからの復旧
4. 経過秒数の出力（`drill_elapsed_seconds`）

## 検証項目

- RPO: 最新バックアップの時刻が目標以内か
- RTO: 障害検知から復旧完了までが 5 分以内か
- 復旧後の整合性:
  - キュー件数が想定範囲内
  - SMTP受信/配送ワーカーが再開し配送が進む
  - `/healthz` と `/slo` が正常

## ロールバック

- 復旧データに異常がある場合は直前の正常バックアップに再リストア
- 影響範囲が広い場合はメール受信を一時停止し、キュー整合性を優先確認する
