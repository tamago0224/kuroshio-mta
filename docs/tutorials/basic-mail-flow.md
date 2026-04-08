# 最小メールフローを試す

`kuroshio-mta` を起動して、SMTP で 1 通受け取り、ローカル queue に入るところまでを確認する最小ハンズオンです。

## 前提

- [Getting Started](/getting-started) を完了している
- `queue_backend: local` で起動している
- `listen_addr` が `:2525` になっている

## 1. MTA を起動する

```bash
go run ./cmd/kuroshio -config ./config.yaml
```

## 2. SMTP で 1 通投入する

別ターミナルで `nc` を使い、最小の SMTP セッションを流します。

```bash
cat <<'EOF' | nc 127.0.0.1 2525
EHLO localhost
MAIL FROM:<sender@example.net>
RCPT TO:<user@example.com>
DATA
Subject: kuroshio test

hello from kuroshio-mta
.
QUIT
EOF
```

成功すると、`MAIL FROM` / `RCPT TO` / `DATA` に対して `250` または `354` が返ります。

## 3. ローカル queue を確認する

`queue_backend: local` の場合、受信したメッセージは `queue_dir` 配下に JSON で保存されます。

```bash
find ./var/queue -maxdepth 2 -type f | sort
```

通常は `mail.inbound` や `mail.retry` などのディレクトリが見えます。

## 4. 次に見る場所

- 設定を見直す: [Configuration](/configuration)
- Admin API で queue を操作する: [Admin API を試す](/tutorials/admin-operations)
- レート制限を加える: [Rate Limit を試す](/tutorials/rate-limit)

## 補足

- 送信先 MX が到達不能な環境では、worker が再送キューへ移すことがあります
- SMTP コマンドの RFC 要件を確認したい場合は、`go test ./internal/smtp -run '^TestSMTPConformance$' -v` を使えます

