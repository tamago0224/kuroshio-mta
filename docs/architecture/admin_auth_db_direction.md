# Admin Auth DB Direction

issue: #196

`kuroshio-mta` の Admin API は、現在 `admin_tokens` / `MTA_ADMIN_TOKENS(_FILE)` を使った config ベース認証です。
このメモでは、そこから DB-backed 認証へ進める方針を整理します。

## 現在の位置

現在の Admin API は次の特徴を持ちます。

- Bearer token で認証
- role は `viewer` / `operator` / `admin`
- token source は config / environment / file
- token は plain text または `sha256=<hex>` で指定可能
- 運用操作は audit log に残る

これは小規模運用には十分ですが、複数ノードや長期運用では次が弱くなります。

- token の発行 / 失効 / ローテーションをその場で反映しづらい
- ノードごとに設定同期が必要
- actor と credential lifecycle を一元管理しづらい
- 今後の RBAC 拡張や OIDC 連携の前段としては弱い

## 目標

中期目標は、Admin API の認証情報を DB 管理へ寄せることです。

ここで重要なのは、いきなり OIDC に振り切るのではなく、
まずは今の Bearer token モデルを DB-backed に置き換えられる形にすることです。

## 段階的な進め方

### Phase 1: config ベースを維持しつつ改善する

これは既に一部進んでいます。

- plain text token に加えて `sha256=<hex>` をサポート
- runbook と config example を hash 形式前提へ更新

この段では後方互換を最優先にします。

### Phase 2: DB-backed token store を追加する

Bearer token 自体は維持しつつ、保存先を DB へ移します。

最初に必要な機能:

- token hash の照合
- role 取得
- `enabled` / `expires_at` による失効制御
- actor 情報や説明の参照
- last used timestamp の更新

この段では Admin API の client 体験はほぼ変えず、運用性だけ上げるのが狙いです。

### Phase 3: identity provider と接続しやすい形へ整理する

将来的に OIDC / OAuth2 / SSO 連携を考えるなら、
Admin API server は「どの backend から actor を得たか」を意識できる構造にしておく方がよいです。

そのため、長期的には次の責務分離を前提にします。

- bearer token parser
- admin auth backend
- role / authorization evaluator
- audit actor mapper

## 想定スキーマ

最初の DB-backed token store としては、少なくとも次のテーブルがあれば十分です。

### `admin_principals`

- `id`
- `name`
- `role`
- `enabled`
- `description`
- `created_at`
- `updated_at`

### `admin_tokens`

- `id`
- `principal_id`
- `token_hash`
- `enabled`
- `expires_at`
- `last_used_at`
- `created_at`
- `updated_at`

最初は `principal` と `token` を分けるだけでも価値があります。

これにより:

- 1 actor に複数 token を発行できる
- token を個別に失効できる
- actor 単位の一時停止もできる

## role model

role は当面、現在の 3 段のまま維持します。

- `viewer`
- `operator`
- `admin`

`admin` は今の実装では `operator` と実質同等ですが、
DB-backed 化するときも「将来の危険操作用の上位 role」として予約しておく方針を維持します。

初回の DB 化では細粒度 permission matrix までは導入しません。

## token 保存方式

DB でも token は平文保存しません。

最低限:

- `SHA-256` などの一方向 hash で保存
- 比較は constant-time

将来的には token prefix と hash を組み合わせて、
運用者が token を識別しやすい形にする余地があります。

## 認証 backend の分割

長期的には `internal/admin` の認証責務を次へ分けられる構造が自然です。

- `config token backend`
- `db token backend`
- `oidc / external identity backend`

Admin API 本体は

- request から bearer token を読む
- backend に照会して principal を得る
- role を見て authorize する

だけを知っていればよい形を目指します。

## 監査との接続

DB-backed 化で特に良くしたいのは audit log との接続です。

少なくとも次が取りやすくなります。

- `actor`
- `principal_id`
- `role`
- `token_id` または token fingerprint
- `last_used_at`

これにより、現在の `actorFromRequest` だけに頼るより、
誰の credential が使われたかを安定して追えます。

## migration

移行は次の順番を想定します。

1. config backend を残したまま DB backend を追加
2. 起動設定で backend 選択を可能にする
3. 本番で DB backend へ切り替える
4. 必要なら config backend を fallback に縮退

この順番なら、いきなり既存運用を壊さずに進められます。

## 非目標

初回の DB-backed 化では次は目指しません。

- Web UI や self-service token 発行画面
- 細粒度の policy engine
- OIDC login そのもの
- session cookie ベース認証

まずは「今の Bearer token 運用を DB へ移す」ことに集中します。

## OIDC との関係

長期的に OIDC を入れるとしても、
DB-backed token store は無駄になりません。

理由:

- machine-to-machine token や break-glass token を保持できる
- 外部 IdP 障害時の fallback を持てる
- audit actor と internal principal の対応を持てる

つまり、DB-backed token store は OIDC の前段でもあり、補完でもあります。

## 結論

`kuroshio-mta` の Admin 認証は、

- 短期: config backend 維持 + hash token
- 中期: DB-backed Bearer token auth
- 長期: OIDC / external identity backend 追加

の順で進めるのが自然です。

特に中期では、「認証方式を変える」のではなく、
「今の Bearer token モデルを DB-backed principal/token 管理へ載せ替える」
と考えるのが実装上も運用上も分かりやすいです。
