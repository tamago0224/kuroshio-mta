# 最小メールフローを試す

`kuroshio-mta` を起動して、SMTP で 1 通受け取り、ローカル queue に入るところまでを確認する最小ハンズオンです。
このページでは `docker compose` で必要な環境を用意し、最小限の設定で動作確認する手順を扱います。

## 前提

- [Getting Started](/getting-started) を完了している
- Docker と `docker compose` が使える

## 1. tutorial 用の compose を起動する

この tutorial 用に、最小設定の `compose.yaml` と `config.yaml` を
`examples/tutorials/basic-mail-flow/` に用意しています。

```bash
mkdir -p examples/tutorials/basic-mail-flow/var/queue
docker compose -f examples/tutorials/basic-mail-flow/compose.yaml up --build -d
```

この構成では次を起動します。

- `kuroshio`: tutorial 用設定で起動する `kuroshio-mta`
- `smtp-client`: SMTP セッションを流すための補助コンテナ

最小設定のポイント:

- `queue_backend: local`
- `queue_dir: /var/lib/kuroshio/queue`
- `listen_addr: :2525`
- `observability_addr: :9090`
- `scan_interval: 1m`

## 2. SMTP で 1 通投入する

```bash
docker compose -f examples/tutorials/basic-mail-flow/compose.yaml exec smtp-client sh -lc "cat <<'EOF' | nc kuroshio 2525
EHLO localhost
MAIL FROM:<sender@example.net>
RCPT TO:<user@example.com>
DATA
Subject: kuroshio test

hello from kuroshio-mta
.
QUIT
EOF"
```

成功すると、`MAIL FROM` / `RCPT TO` / `DATA` に対して `250` または `354` が返ります。

## 3. ローカル queue を確認する

```bash
find examples/tutorials/basic-mail-flow/var/queue -maxdepth 2 -type f | sort
```

`queue_backend: local` の場合、受信したメッセージは `queue_dir` 配下に JSON で保存されます。
この tutorial では bind mount しているので、ホスト側の
`examples/tutorials/basic-mail-flow/var/queue` を直接確認できます。
通常は `mail.inbound` や `mail.retry` などのディレクトリが見えます。

## 4. Observability を確認する

```bash
curl http://127.0.0.1:9090/metrics | head
```

Prometheus 形式のメトリクスが返れば、listener と observability endpoint の両方が動いています。

## 5. 後片付け

```bash
docker compose -f examples/tutorials/basic-mail-flow/compose.yaml down
```

queue の中身も消したい場合は次を実行します。

```bash
rm -rf examples/tutorials/basic-mail-flow/var
```

## 6. 次に見る場所

- 設定を見直す: [Configuration](/configuration)
- Admin API で queue を操作する: [Admin API を試す](/tutorials/admin-operations)
- レート制限を加える: [Rate Limit を試す](/tutorials/rate-limit)

## 補足

- 送信先 MX が到達不能な環境では、worker が再送キューへ移すことがあります
- SMTP コマンドの RFC 要件を確認したい場合は、`go test ./internal/smtp -run '^TestSMTPConformance$' -v` を使えます
