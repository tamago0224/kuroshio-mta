# Admin API Runbook

Issue: #36

## 目的

- 再送キューや `mail.dlq` の再投入を手作業ファイル操作なしで実施する
- suppression list を API 経由で安全に操作する
- 操作を監査ログへ残す

## 有効化

config token backend:

- `MTA_ADMIN_ADDR=:9091`
- `MTA_ADMIN_TOKENS=sha256=d036bd6d01a1cae081d39a2f8dab751dc042de814fd60df31fcb553170950f29:viewer,sha256=0850123315d21ab90f4f7236408a52ef6dbd6a02a6550e5c10dc73f4d993680e:operator`

`MTA_ADMIN_TOKENS` は `token:role` のカンマ区切り、または `sha256=<hex>:role` のカンマ区切りで指定する。
短期的には hash 形式を推奨する。

例:

```bash
printf 'viewer-token' | sha256sum
printf 'operator-token' | sha256sum
```

SQLite backend:

- `MTA_ADMIN_ADDR=:9091`
- `MTA_ADMIN_AUTH_BACKEND=sqlite`
- `MTA_ADMIN_AUTH_DSN=file:./var/admin-auth.db`

SQLite backend は次の 2 テーブルを参照する。

- `admin_principals(id, name, role, enabled, description, created_at, updated_at)`
- `admin_tokens(id, principal_id, token_hash, enabled, expires_at, last_used_at, created_at, updated_at)`

`token_hash` には Bearer token の SHA-256 hex を保存する。
`expires_at` は SQLite が `datetime()` で解釈できる形式で保存する。
認証成功時は `last_used_at` を更新する。

## 権限

- `viewer`: 一覧参照
- `operator`: 一覧参照、requeue、suppression 追加/削除
- `admin`: 現時点では `operator` と同等。将来の危険操作用に予約

## API

- `GET /api/v1/suppressions`
- `POST /api/v1/suppressions`
- `DELETE /api/v1/suppressions/{address}`
- `GET /api/v1/queue/{state}?limit=50`
- `POST /api/v1/queue/{state}/{message_id}/requeue`

`state` は `retry` または `dlq` を想定する。

## dry-run

- suppression 削除:
  `DELETE /api/v1/suppressions/user@example.com?dry_run=1`
- 再投入:
  `POST /api/v1/queue/dlq/msg-1/requeue?dry_run=1`

## CLI

```bash
export KUROSHIO_ADMIN_URL=http://127.0.0.1:9091
export KUROSHIO_ADMIN_TOKEN=operator-token
export KUROSHIO_ADMIN_ACTOR=oncall@example.com

scripts/admin/kuroshio_admin.sh list-suppressions
scripts/admin/kuroshio_admin.sh add-suppression user@example.com manual
scripts/admin/kuroshio_admin.sh list-queue dlq 20
scripts/admin/kuroshio_admin.sh requeue dlq msg-1 --dry-run
```

## 監査ログ

- すべての運用操作は `component=audit` で JSON ログ出力
- `event`, `actor`, `actor_header`, `auth_principal`, `auth_role`, `auth_source`, `token_fingerprint`, `path`, `message_id`, `dry_run` を含む
- `X-Admin-Actor` は人間や runbook 上の実行主体を表し、`auth_principal` は認証 backend が返す principal を表す
- token の平文は出力せず、`token_fingerprint` には安全な識別子だけを出す
- SQLite backend では `auth_source=sqlite_token` となる

## 制約

- 現時点の queue 操作はローカルファイルバックエンド専用
- Kafka バックエンドの運用APIは別対応

将来の DB-backed Admin 認証方針は
[Admin Auth DB Direction](/architecture/admin_auth_db_direction)
を参照してください。

## 配送失敗分類と自動 suppression

SMTP の恒久失敗と、宛先を以後も抑止する判断は分けています。
自動登録するのは **RCPT 段階の 5xx 応答で enhanced status が `5.1.1`** の場合だけです。
MAIL の送信者エラー、DATA の `550 5.7.1`、認証・TLS ポリシーエラー、容量不足、
理由のない550、段階不明のエラーは宛先無効とみなしません。
複数行応答はすべて読み、enhanced status が欠落・不一致の場合も自動登録しません。

| 分類 | 既定の判断例 | 自動 suppression |
| --- | --- | --- |
| `invalid_recipient` | RCPT の `5.1.1` | あり |
| `mailbox_full` | `4.2.2` / `5.2.2` | なし |
| `policy` | TLS・DANE・MTA-STS、その他 `X.7.*` | なし |
| `auth` | AUTH 段階、530 / 535、`X.7.8` / `X.7.9` | なし |
| `dns` | DNS 解決エラー、`X.4.3` | なし |
| `transport` | ネットワーク・接続切断、その他 `X.4.*` | なし |
| `unknown` | 段階不明・enhanced status 不明・未対応の理由 | なし |

`spam` は分類名を予約していますが、既定のルールはありません。`5.7.1` だけから spam と断定しません。
分類ルールは `internal/delivery/classification.go` に集約しています。provider 固有のルールを追加する場合は
識別可能なルール名と回帰テストを追加し、曖昧な応答テキストから自動 suppression を有効化しないでください。
現時点では外部ルールファイルや追加の設定項目はありません。

分類によって SMTP コードを書き換えません。既存の4xx再送、5xx恒久失敗、DSN生成、
試行回数・再送期限の扱いを維持します。DSN の MIME / status 形式と `suppression.json` の JSON 形式も変更していません。
既存 suppression エントリは自動削除しないため、過去の誤登録が疑われる場合は次の手順で確認してください。

1. `GET /api/v1/suppressions` で理由を確認し、必要なら保存データをバックアップします。
2. 監査イベント `delivery_failure_classified` の `category`、`rule`、`stage`、`smtp_code`、
   `enhanced_status`、`suppression_eligible` を確認します。宛先はマスクし、応答本文をこのイベントには記録しません。
3. `worker_bounce_class_<分類>_total` と `worker_bounce_rule_<ルール>_total` で増加傾向を確認します。
   `worker_suppression_added_total` は保存に成功した自動登録回数です。既存抑止のスキップは `worker_suppressed_recipient_total` で確認します。
4. 誤登録と判断した宛先だけを、既存の `DELETE /api/v1/suppressions/{address}?dry_run=1` で対象確認後に削除します。
   この変更自体は既存の抑止を解除しません。

分類の根拠は [RFC 3463](https://www.rfc-editor.org/rfc/rfc3463.html) と
[IANA enhanced status registry](https://www.iana.org/assignments/smtp-enhanced-status-codes/) を参照してください。

## Policy quarantine

ローカルキューで隔離を有効化した場合は、
[設定・期限・管理操作](../configuration.md#policy-quarantine-の運用) を参照してください。
viewer は `GET /api/v1/queue/quarantine` で本文を含まない一覧を確認できます。
operator は `POST /api/v1/queue/quarantine/{id}/requeue` で解放、
`DELETE /api/v1/queue/quarantine/{id}` で削除できます。両操作とも `?dry_run=1` を利用できます。
期限切れは scan 時に配送せず削除するため、`queue_quarantine_messages` の増加を監視し、
誤判定があれば期限内に対象 ID と判定元を確認してください。
解放後は通常の配送・再送制御が適用されます。期限切れと隔離中の削除は DSN を生成しません。
