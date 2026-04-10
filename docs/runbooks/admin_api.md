# Admin API Runbook

Issue: #36

## 目的

- 再送キューや `mail.dlq` の再投入を手作業ファイル操作なしで実施する
- suppression list を API 経由で安全に操作する
- 操作を監査ログへ残す

## 有効化

- `MTA_ADMIN_ADDR=:9091`
- `MTA_ADMIN_TOKENS=sha256=d036bd6d01a1cae081d39a2f8dab751dc042de814fd60df31fcb553170950f29:viewer,sha256=0850123315d21ab90f4f7236408a52ef6dbd6a02a6550e5c10dc73f4d993680e:operator`

`MTA_ADMIN_TOKENS` は `token:role` のカンマ区切り、または `sha256=<hex>:role` のカンマ区切りで指定する。
短期的には hash 形式を推奨する。

例:

```bash
printf 'viewer-token' | sha256sum
printf 'operator-token' | sha256sum
```

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
export KUROSHIO_ADMIN_URL=http://127.0.0.1:9091
export KUROSHIO_ADMIN_TOKEN=operator-token
export KUROSHIO_ADMIN_ACTOR=oncall@example.com

scripts/admin/kuroshio_admin.sh list-suppressions
scripts/admin/kuroshio_admin.sh add-suppression user@example.com manual
scripts/admin/kuroshio_admin.sh list-queue dlq 20
scripts/admin/kuroshio_admin.sh requeue dlq msg-1 --dry-run
```

## 監査ログ

- すべての運用操作は `component=audit` で JSON ログ出力
- `event`, `actor`, `path`, `message_id`, `dry_run` を含む

## 制約

- 現時点の queue 操作はローカルファイルバックエンド専用
- Kafka バックエンドの運用APIは別対応

将来の DB-backed Admin 認証方針は
[Admin Auth DB Direction](/architecture/admin_auth_db_direction)
を参照してください。
