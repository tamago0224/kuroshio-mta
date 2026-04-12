# Submission Auth を試す

Submission の `AUTH PLAIN` / `AUTH LOGIN` を、SQLite-backed credential と sender scope つきで確認する tutorial です。

## 1. tutorial 用の compose を起動する

この tutorial 用に、`compose.yaml` / `config.yaml` / `init.sql` を
`examples/tutorials/submission-auth/` に用意しています。

```bash
mkdir -p examples/tutorials/submission-auth/var/queue
docker compose -f examples/tutorials/submission-auth/compose.yaml up --build -d
```

## 2. Submission AUTH の成功例を確認する

```bash
docker compose -f examples/tutorials/submission-auth/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 587
EHLO tutorial.local
AUTH PLAIN AGFsaWNlQGV4YW1wbGUuY29tAHMzY3IzdA==
MAIL FROM:<billing@example.net>
RCPT TO:<receiver@example.org>
DATA
Subject: submission auth tutorial

hello from submission auth
.
QUIT
EOF"
```

`AUTH` が `235`、`MAIL FROM` が `250` で返れば成功です。

## 2.1 AUTH LOGIN の成功例を確認する

```bash
docker compose -f examples/tutorials/submission-auth/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 587
EHLO tutorial.local
AUTH LOGIN
YWxpY2VAZXhhbXBsZS5jb20=
czNjcjN0
MAIL FROM:<billing@example.net>
RCPT TO:<receiver@example.org>
DATA
Subject: submission auth login tutorial

hello from submission auth login
.
QUIT
EOF"
```

`AUTH` が `235`、`MAIL FROM` が `250` で返れば成功です。

## 3. sender scope mismatch を確認する

`allowed_sender_domains` / `allowed_sender_addresses` に含まれない送信元は reject されます。

```bash
docker compose -f examples/tutorials/submission-auth/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 587
EHLO tutorial.local
AUTH PLAIN AGFsaWNlQGV4YW1wbGUuY29tAHMzY3IzdA==
MAIL FROM:<alice@other.example>
QUIT
EOF"
```

`MAIL FROM` が `553` で reject されれば期待どおりです。

## 4. 認証失敗を確認する

```bash
docker compose -f examples/tutorials/submission-auth/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 587
EHLO tutorial.local
AUTH PLAIN AGFsaWNlQGV4YW1wbGUuY29tAHdyb25n
QUIT
EOF"
```

`AUTH` が `535` で返れば認証失敗です。

## 4.1 AUTH LOGIN の失敗例を確認する

```bash
docker compose -f examples/tutorials/submission-auth/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 587
EHLO tutorial.local
AUTH LOGIN
YWxpY2VAZXhhbXBsZS5jb20=
d3Jvbmc=
QUIT
EOF"
```

`AUTH` が `535` で返れば認証失敗です。

## 5. trace / log を確認する

- trace: `smtp.auth` span
- log: `submission auth succeeded`
- log: `submission auth failed`
- log: `submission sender identity rejected`

詳しい読み方は [Submission Auth Runbook](/runbooks/submission_auth) を参照してください。

## 6. static backend に切り替えて試す

SQLite を使わず、`submission_users` の static backend でも同じフローが確認できます。

`examples/tutorials/submission-auth/config.yaml` を以下のように書き換えて再起動します。

```yaml
submission_auth_backend: static
submission_users: "alice@example.com:s3cr3t"
submission_auth_dsn: ""
```

再起動:

```bash
docker compose -f examples/tutorials/submission-auth/compose.yaml restart kuroshio
```

以降は同じ手順で `AUTH PLAIN` / `AUTH LOGIN` を確認できます。

## FAQ

### `expires_at` の書式は?

SQLite の `datetime()` で解釈できる形式を使います。
例: `2026-04-13 12:00:00`

### `last_auth_at` はいつ更新される?

認証成功時に更新されます。失敗時は更新されません。

## 後片付け

```bash
docker compose -f examples/tutorials/submission-auth/compose.yaml down
rm -rf examples/tutorials/submission-auth/var
```

## 次に読む

- [Tutorials Home](/tutorials/)
- [RFC 4954: SMTP AUTH](/rfc_4954_gap)
- [Submission Auth Runbook](/runbooks/submission_auth)
