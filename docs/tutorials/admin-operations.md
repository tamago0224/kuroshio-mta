# Admin API を試す

`kuroshio-mta` の運用向け API を使って、queue の確認や requeue、suppression 操作を試すハンズオンです。
このページでは `docker compose` で最小環境を立ち上げて、実際に API と付属 CLI を試します。

## 前提

- [Getting Started](/getting-started) を完了している
- `queue_backend: local` を使っている
- Docker と `docker compose` が使える

## 1. tutorial 用の compose を起動する

この tutorial 用に、最小設定の `compose.yaml` と `config.yaml` を
`examples/tutorials/admin-operations/` に用意しています。

```bash
mkdir -p examples/tutorials/admin-operations/var/queue
docker compose -f examples/tutorials/admin-operations/compose.yaml up --build -d
```

この構成では、Admin API は `http://127.0.0.1:9091` に公開されます。
token は次を使います。

- `viewer-token`
- `operator-token`

## 2. queue に 1 通入れる

```bash
docker compose -f examples/tutorials/admin-operations/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 2525
EHLO localhost
MAIL FROM:<sender@example.net>
RCPT TO:<user@example.com>
DATA
Subject: admin tutorial

hello from admin tutorial
.
QUIT
EOF"
```

## 3. 付属 CLI を使う

```bash
export KUROSHIO_ADMIN_URL=http://127.0.0.1:9091
export KUROSHIO_ADMIN_TOKEN=operator-token
export KUROSHIO_ADMIN_ACTOR=oncall@example.com

scripts/admin/kuroshio_admin.sh list-suppressions
scripts/admin/kuroshio_admin.sh list-queue dlq 20
```

状況によっては `mail.inbound` にメッセージが見えるので、必要なら runbook の手順とあわせて確認します。

## 4. dry-run で安全に試す

いきなり本操作をせず、まず dry-run で確認できます。

```bash
scripts/admin/kuroshio_admin.sh requeue dlq msg-1 --dry-run
```

## 5. HTTP で直接見る

```bash
curl -H 'Authorization: Bearer viewer-token' \
  http://127.0.0.1:9091/suppressions
```

## 6. 後片付け

```bash
docker compose -f examples/tutorials/admin-operations/compose.yaml down
rm -rf examples/tutorials/admin-operations/var
```

## 7. 制約

- queue 操作は現時点ではローカルファイルバックエンド専用です
- Kafka バックエンドの運用 API は別対応です

## 次に読むページ

- API 詳細: [Admin API Runbook](/runbooks/admin_api)
- Kafka で動かす: [Kafka Queue モード](/kafka_queue_mode)
- reputation 運用: [Reputation Ops](/runbooks/reputation_ops)
