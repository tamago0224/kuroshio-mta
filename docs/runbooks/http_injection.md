# HTTP Message Injection

認証済みアプリケーションから raw RFC 5322 メッセージを永続キューへ投入する HTTPS API の運用手順です。MIME生成、テンプレート展開、一括パーソナライズはアプリケーション側の責務です。

## 有効化

SMTP Submission と同じ認証 backend を使用します。listener は TLS 専用で、平文 HTTP への切替設定はありません。

```yaml
submission_auth_backend: static
submission_users: "mailer@example.test:replace-me"
submission_enforce_sender_identity: true
tls_cert_file: /run/secrets/tls.crt
tls_key_file: /run/secrets/tls.key
http_injection:
  enabled: true
  listen_addr: "127.0.0.1:8443"
  idempotency_ttl: 24h
  rate_limit_per_minute: 60
```

## リクエスト

Basic 認証では Submission の username/password を使います。OAuth introspection を設定した環境では Bearer token と、token 内 mailbox と照合する `X-Submission-User` を指定できます。本文、password、Bearer token、idempotency key はログへ記録しません。

```bash
curl --fail-with-body \
  --user 'mailer@example.test:replace-me' \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: order-20261025-0001' \
  --data-binary @- \
  https://127.0.0.1:8443/api/v1/messages <<'JSON'
{
  "envelope_from": "mailer@example.test",
  "recipients": ["user@recipient.test"],
  "raw_message": "From: mailer@example.test\r\nTo: user@recipient.test\r\nSubject: test\r\n\r\nhello\r\n"
}
JSON
```

成功時は durable enqueue と idempotency record の確定後に `202` と `message_id` を返します。同じ principal・key・内容の再送は `200` と同じ ID、`replayed: true` を返します。同じ key で内容が異なる場合は `409` です。部分受理は行わず、宛先または enqueue の一部でも失敗すれば成功応答しません。

## 正規化と権限

- envelope sender と全 recipient は SMTPUTF8 を使わない mailbox として検証する
- raw message は CRLF に正規化し、1行998 octet、`max_message_bytes`、ヘッダーと本文の区切りを検証する
- envelope sender と単一 From author の両方に認証 principal の sender scope を適用する
- 認証 principal を trusted tenant、traffic class を `transactional` として保存する
- HTTP 入力では外部 SMTP の SPF / DKIM / DMARC 受信評価を行わない

## Idempotency と障害境界

記録は `queue_dir/http-injection-idempotency/` に本文を含めず保存し、TTL 後に同じ key を新規投入として扱います。local queue では安定した message ID と queue 側の重複排除により、プロセス停止後の再送も1件になります。

Kafka は idempotency record と broker publish を単一 transaction にできないため、publish 成功直後かつ record 確定前にプロセスが停止すると、再送時に同じ message ID のレコードが複数 publish される可能性があります。consumer は message ID を冪等キーとして扱ってください。この API は少なくとも1回の投入であり、exactly-once を保証しません。

## 応答

| status | 意味 |
| --- | --- |
| `200` | 同一内容の再送 |
| `202` | 新規に永続キューへ投入 |
| `400` | JSON または idempotency key が不正 |
| `401` | 認証失敗 |
| `409` | 同じ idempotency key に異なる内容 |
| `415` | `application/json` 以外 |
| `422` | envelope、recipient、raw message、sender scope が不正 |
| `429` | principal 単位のレート上限 |
| `503` | queue または idempotency store の一時失敗 |

`503` と接続断では同じ `Idempotency-Key` で再送します。新しい key に変えると別メッセージとして扱われます。
