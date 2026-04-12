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
- `invalid_password`
  password hash が一致しない
- `credential_disabled`
  credential はあるが `enabled=false`
- `credential_expired`
  `expires_at` を過ぎている
- `empty_credentials`
  backend 呼び出し時点で username または password が空
- `backend_unavailable`
  backend 初期化や注入に失敗している
- `backend_error`
  SQLite lookup や expiry check の内部エラー

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

## SQLite backend の確認ポイント

- `submission_credentials.last_auth_at` が更新されているか
- `allowed_sender_domains` / `allowed_sender_addresses` の値が意図どおりか
- `expires_at` が SQLite `datetime()` で解釈できる形式か

## 関連ドキュメント

- [Configuration](/configuration)
- [RFC 4954 Gap Note](/rfc_4954_gap)
- [SMTP AUTH Modern Auth Direction](/architecture/smtp_auth_modern_auth)
