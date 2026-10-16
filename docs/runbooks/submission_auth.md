# Submission Auth Runbook

Issue: #251

## 目的

- Submission 認証失敗の原因を trace / log から切り分ける
- static backend と SQLite backend の違いを運用時に判別する
- sender identity 制約と sender scope 制約の効き方を確認する

## 見る場所

- trace: `smtp.auth` span
- trace: `smtp.mail` span の reject reason
- log: `submission auth succeeded`
- log: `submission auth failed`
- log: `submission sender identity rejected`

## `smtp.auth` span で見る項目

- `smtp.auth.mechanism`
- `smtp.auth.success`
- `smtp.auth.user`
- `smtp.auth.backend`
- `smtp.auth.failure_reason`
- `smtp.auth.sender_scope_mode`
- `smtp.auth.allowed_sender_domain_count`
- `smtp.auth.allowed_sender_address_count`

## backend 種別

- `static_password`
  `submission_users` / `MTA_SUBMISSION_USERS(_FILE)` を使う
- `sqlite_password`
  `submission_auth_backend=sqlite` と `submission_auth_dsn` を使う

## failure reason

- `credential_not_found`
  username に対応する credential がない
- 次のアクション: username と `submission_credentials.username` の一致を確認する
- `invalid_password`
  password hash が一致しない
- 次のアクション: `password_hash` を再生成して更新する
- `credential_disabled`
  credential はあるが `enabled=false`
- 次のアクション: 意図した停止か確認し、必要なら `enabled=1` に戻す
- `credential_expired`
  `expires_at` を過ぎている
- 次のアクション: `expires_at` を更新するか、無期限なら `NULL` にする
- `empty_credentials`
  backend 呼び出し時点で username または password が空
- 次のアクション: SMTP クライアントの設定を確認する
- `backend_unavailable`
  backend 初期化や注入に失敗している
- 次のアクション: `submission_auth_backend` / `submission_auth_dsn` を確認する
- `backend_error`
  SQLite lookup や expiry check の内部エラー
- 次のアクション: SQLite のファイル権限・ロック状態・DB 破損を確認する

SMTP client への応答は従来どおり一般化されており、
失敗時は基本的に `535 authentication credentials invalid` を返す。
詳細は trace / log で確認する。

## sender scope mode

- `username_domain_fallback`
  sender scope 未設定で、認証 username のドメイン一致を使っている
- `credential_scope`
  `allowed_sender_domains` または `allowed_sender_addresses` を使っている

## sender identity reject の見方

`submission sender identity rejected` ログでは次を見る。

- `auth_backend`
- `auth_user`
- `mail_from`
- `sender_scope_mode`
- `reason=sender_scope_mismatch`

trace 側では `smtp.mail` span の reject reason が
`sender_not_allowed_for_auth` になる。

## ログ例

`submission auth succeeded`:

```json
{"time":"2026-04-13T12:00:00Z","level":"INFO","msg":"submission auth succeeded","component":"smtp","auth_backend":"sqlite_password","auth_user":"a***@example.com","mechanism":"PLAIN","sender_scope_mode":"credential_scope","allowed_sender_domain_count":1,"allowed_sender_address_count":1}
```

`submission auth failed`:

```json
{"time":"2026-04-13T12:00:01Z","level":"WARN","msg":"submission auth failed","component":"smtp","auth_backend":"static_password","auth_user":"a***@example.com","mechanism":"LOGIN","failure_reason":"invalid_password"}
```

`submission sender identity rejected`:

```json
{"time":"2026-04-13T12:00:02Z","level":"WARN","msg":"submission sender identity rejected","component":"smtp","reason":"sender_scope_mismatch","auth_backend":"sqlite_password","auth_user":"a***@example.com","mail_from":"b***@other.example","sender_scope_mode":"credential_scope"}
```

## SQLite backend の確認ポイント

- `submission_credentials.last_auth_at` が更新されているか
- `allowed_sender_domains` / `allowed_sender_addresses` の値が意図どおりか
- `expires_at` が SQLite `datetime()` で解釈できる形式か

## 関連ドキュメント

- [Configuration](/configuration)
- [RFC 4954 Gap Note](/rfc_4954_gap)
- [SMTP AUTH Modern Auth Direction](/architecture/smtp_auth_modern_auth)

## XOAUTH2 と外部 IdP

RFC 7662 の access-token introspection を提供する IdP を事前に用意します。
client credentials は introspection 専用に権限を限定し、token の発行・失効・consent は IdP で管理します。
本実装は Google / Microsoft 等への接続互換性を保証する provider adapter ではありません。
JWT のローカル署名検証、JWKS、OAUTHBEARER、refresh token の処理は提供しません。

```yaml
submission_addr: ":587"
submission_auth_required: true
submission_auth_backend: static
submission_users: "" # XOAUTH2 のみ。既存 users を指定すれば PLAIN/LOGIN を併用
submission_enforce_sender_identity: true
tls_cert_file: /run/secrets/submission.crt
tls_key_file: /run/secrets/submission.key
submission_bearer_introspection_url: https://idp.example/oauth/introspect
submission_bearer_client_id: kuroshio-submission
submission_bearer_issuer: https://idp.example
submission_bearer_audience: smtp-submission
submission_bearer_username_claim: email
submission_bearer_required_scope: smtp.send
```

`MTA_SUBMISSION_BEARER_CLIENT_SECRET_FILE=/run/secrets/introspection-client-secret` を設定します。
ファイルと秘密鍵の権限を制限し、token / client secret をログやシェル履歴に残さないでください。
`email` claim を使う IdP は、その claim が所有確認済みのメールボックスを表すよう設定します。
設定値は [設定](../configuration.md) を参照してください。

クライアントの手順:

1. EHLO → STARTTLS → 証明書検証 → EHLO を実行する
2. `AUTH XOAUTH2 <base64>` を送る（初期応答を省略して 334 後に送る形式にも対応）
3. 復号後の応答は `user=<mailbox>\x01auth=Bearer <access-token>\x01\x01`。`\x01` は実際の control-A byte
4. 成功は 235。無効 token は 334 の base64 JSON challenge を受け、空応答（または `*`）を送って 535 を受け取る
5. 認証後、同じ mailbox の MAIL FROM を送る

応答を含む AUTH 行は最大 12 KiB、token は最大 8 KiB です。通常の SMTP command は従来どおり 512 byte です。
過大・途中終了した SASL 応答は接続を終了します。構文不正は 501、TLS 前は 538、
無効 token / IdP 障害は 535 になります。IdP 応答や内部エラーをクライアントへ返しません。

監査は既存の `submission auth succeeded` / `submission auth failed` を使います。
`mechanism=XOAUTH2`、`auth_backend=oauth_introspection`、マスク済み `auth_user` と結果を記録し、
token / client secret / raw IdP response は記録しません。`invalid_token` は claim・期限・scope 等を確認し、
`backend_unavailable` は endpoint の到達性・証明書・client credentials・HTTP status を IdP 側で確認します。

認証結果をキャッシュせず毎回 introspection するため、失効は次回 AUTH に反映します。
既存の認証済み SMTP session は失効時に自動切断しません。IdP 障害時は新規認証を拒否します。

参考: [RFC 7662](https://datatracker.ietf.org/doc/html/rfc7662)、
[XOAUTH2 protocol](https://developers.google.com/workspace/gmail/imap/xoauth2-protocol)。
