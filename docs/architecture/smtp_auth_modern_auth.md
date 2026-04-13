# SMTP AUTH Modern Auth Direction

issue: #197

`kuroshio-mta` の Submission 認証は、現時点では `AUTH PLAIN` / `AUTH LOGIN` を前提にした static / sqlite backend を提供しています。
このメモでは、そこから将来的に bearer token 系 backend を追加して modern auth へ進めるときの設計方針を整理します。

## 現在の位置

現在の実装は次の前提です。

- SMTP AUTH mechanism は `PLAIN` / `LOGIN`
- `submission_auth_backend` で `static` / `sqlite` を切り替える
- `static` は `submission_users` または `MTA_SUBMISSION_USERS(_FILE)` から認証情報を読み込む
- `sqlite` は `submission_auth_dsn` で指定した SQLite を使い、`submission_credentials` テーブルを参照する
- 認証済み user と `MAIL FROM` ドメインの整合は `submission_enforce_sender_identity` と sender scope で制御する

設定値と運用の詳細は [Configuration](/configuration) と [Submission Auth Runbook](/runbooks/submission_auth) を参照してください。

static backend は小規模導入やローカル検証には十分ですが、次の課題があります。

- password のローテーションや失効が運用しづらい
- 複数ノードで設定同期が必要になる
- account の有効期限や一時停止を表現しづらい
- 誰がどの credential を使ったかを監査しづらい

sqlite backend で lifecycle と監査は一定改善しましたが、bearer token や外部 IdP 連携は未対応です。

## 進め方

段階的に進めます。

### Phase 1: 現行 basic auth を安定運用する

- `AUTH PLAIN` / `AUTH LOGIN` は維持
- static backend は後方互換のため残す
- sender identity 制約や trace / log を整備して、今の Submission 経路を安定運用できる状態を維持する

### Phase 2: SMTP AUTH credential を DB 管理へ移す（到達済み）

basic auth 自体は維持しつつ、credential の保存先だけ DB へ寄せる。

現在の sqlite backend で扱う項目:

- `username`
- `password_hash`
- `enabled`
- `expires_at`
- `allowed_sender_domains`
- `allowed_sender_addresses`
- `last_auth_at`

`description` / `created_at` / `updated_at` などは、今後の拡張余地として残しています。

ここでの狙いは、SMTP protocol 上の見え方を変えずに lifecycle と auditability を改善することです。

### Phase 3: bearer token 系 backend を追加する

長期目標として `AUTH XOAUTH2` を検討する。

ただし実装は provider 固有の処理から始めず、
まず「Bearer token を検証できる認証 backend」として設計する。

この段で分離したい責務:

- SMTP AUTH command parser
- mechanism ごとの credential extraction
- bearer token validator
- sender identity / sender scope 判定
- audit log / trace への actor 記録

## XOAUTH2 と OAUTHBEARER の扱い

優先順位は次です。

1. `XOAUTH2`
2. `OAUTHBEARER`

理由:

- `XOAUTH2` は Google / Microsoft 系クライアントとの相互運用を考えると優先度が高い
- `OAUTHBEARER` は標準寄りだが、最初の互換性ターゲットとしては `XOAUTH2` の方が実用上の価値が高い

一方で、内部設計は `XOAUTH2` 固有に寄せすぎない方がよいです。
mechanism と validator を分離しておくことで、将来 `OAUTHBEARER` を追加しやすくなります。

## 想定する backend 分割

長期的には `internal/userauth` を、少なくとも次の責務へ分けられる形にしておくのが自然です。

- `static password backend`
- `db password backend`
- `bearer token backend`

その上で SMTP server 側は

- どの mechanism を advertise するか
- その mechanism に必要な challenge / response をどう処理するか

だけを知っていればよい形を目指します。

## provider 依存ポイント

provider 依存を持ち込みやすいのは次です。

- access token の issuer / audience 検証
- scope / claim の意味づけ
- username と mailbox identity の対応づけ
- refresh token や device flow など、SMTP の外側にある lifecycle

これらは SMTP server 本体へ埋め込まず、
validator または backend adapter 側へ閉じ込める方針にします。

## 非目標

初回の modern auth 対応で次は目指しません。

- provider ごとの完全互換を一度に実装すること
- Submission UI や consent flow を MTA 自身が持つこと
- SMTP AUTH を OAuth 専用へ切り替えること
- MFA や browser-based login を SMTP server 自体へ埋め込むこと

## 移行方針

当面の移行順は次です。

1. static backend を維持したまま DB-backed password backend を追加
2. mechanism advertisement を backend capability から決められる形へ整理
3. bearer token backend を追加
4. 必要に応じて `XOAUTH2` を advertise

この順番なら、既存の Submission client を壊さずに進められます。

## observability と監査

DB 化や modern auth を進める前提として、少なくとも次の記録が必要です。

- 認証成功 / 失敗
- mechanism
- username または actor
- sender identity mismatch
- backend 種別
- token / credential 失効や期限切れ

trace と audit log の両方で actor を追えるようにしておくと、
将来の運用負荷がかなり下がります。

## 結論

`kuroshio-mta` の SMTP AUTH は、

- 短期: `PLAIN` / `LOGIN` 維持
- 中期: DB-backed password auth
- 長期: bearer token backend と `XOAUTH2`

の順で進めるのが自然です。

特に長期では「`XOAUTH2` を実装する」より、
「provider 非依存の bearer token auth backend を持ち、その一つの mechanism として `XOAUTH2` を扱う」
という設計にしておくのが重要です。
