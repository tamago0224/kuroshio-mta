# Admin API を試す

`kuroshio-mta` の運用向け API を使って、queue の確認や requeue、suppression 操作を試すハンズオンです。

## 前提

- [Getting Started](/getting-started) を完了している
- `queue_backend: local` を使っている
- Admin API 用の token を設定している

## 1. Admin API を有効化する

`config.yaml` または環境変数で Admin API を有効化します。

```bash
MTA_ADMIN_ADDR=:9091
MTA_ADMIN_TOKENS=viewer-token:viewer,operator-token:operator
```

## 2. MTA を起動する

```bash
go run ./cmd/kuroshio -config ./config.yaml
```

## 3. 付属 CLI を使う

```bash
export ORINOCO_ADMIN_URL=http://127.0.0.1:9091
export ORINOCO_ADMIN_TOKEN=operator-token
export ORINOCO_ADMIN_ACTOR=oncall@example.com

scripts/admin/orinoco_admin.sh list-suppressions
scripts/admin/orinoco_admin.sh list-queue dlq 20
```

## 4. dry-run で安全に試す

いきなり本操作をせず、まず dry-run で確認できます。

```bash
scripts/admin/orinoco_admin.sh requeue dlq msg-1 --dry-run
```

## 5. 制約

- queue 操作は現時点ではローカルファイルバックエンド専用です
- Kafka バックエンドの運用 API は別対応です

## 次に読むページ

- API 詳細: [Admin API Runbook](/runbooks/admin_api)
- Kafka で動かす: [Kafka Queue モード](/kafka_queue_mode)
- reputation 運用: [Reputation Ops](/runbooks/reputation_ops)

