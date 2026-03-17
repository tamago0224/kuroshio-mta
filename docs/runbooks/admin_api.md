# Admin API Runbook

Issue: #36

## 目的

- 再送キューや `mail.dlq` の再投入を手作業ファイル操作なしで実施する
- suppression list を API 経由で安全に操作する
- 操作を監査ログへ残す

## 有効化

- `MTA_ADMIN_ADDR=:9091`
- `MTA_ADMIN_TOKENS=viewer-token:viewer,operator-token:operator,admin-token:admin`

`MTA_ADMIN_TOKENS` は `token:role` のカンマ区切りで指定する。

## 権限

- `viewer`: 一覧参照
- `operator`: 一覧参照、requeue、suppression 追加/削除
- `admin`: 現時点では `operator` と同等。将来の危険操作用に予約

## API

- `GET /api/v1/suppressions`
- `POST /api/v1/suppressions`
- `DELETE /api/v1/suppressions/{address}`
- `GET /api/v1/queue/{state}?limit=50`
- `POST /api/v1/queue/{state}/{message_id}/requeue`

`state` は `retry` または `dlq` を想定する。

## dry-run

- suppression 削除:
  `DELETE /api/v1/suppressions/user@example.com?dry_run=1`
- 再投入:
  `POST /api/v1/queue/dlq/msg-1/requeue?dry_run=1`

## CLI

```bash
export ORINOCO_ADMIN_URL=http://127.0.0.1:9091
export ORINOCO_ADMIN_TOKEN=operator-token
export ORINOCO_ADMIN_ACTOR=oncall@example.com

scripts/admin/orinoco_admin.sh list-suppressions
scripts/admin/orinoco_admin.sh add-suppression user@example.com manual
scripts/admin/orinoco_admin.sh list-queue dlq 20
scripts/admin/orinoco_admin.sh requeue dlq msg-1 --dry-run
```

## 監査ログ

- すべての運用操作は `component=audit` で JSON ログ出力
- `event`, `actor`, `path`, `message_id`, `dry_run` を含む

## 制約

- 現時点の queue 操作はローカルファイルバックエンド専用
- Kafka バックエンドの運用APIは別対応
