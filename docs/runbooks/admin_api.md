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
- `GET /api/v1/delivery/history?message_id={id}&result={result}&since={RFC3339}&until={RFC3339}&limit=50&after={cursor}`

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
scripts/admin/kuroshio_admin.sh delivery-history msg-1 50
```

## 配送履歴

`delivery_history.enabled: true` のとき、viewer 以上は受理時刻と宛先別の配送結果を検索できます。
結果は `accepted`、`deferred`、`retry`、`delivered`、`failed`、`unknown` です。レスポンスの
`next_after` を次の `after` に指定して続きを取得します。1ページは最大200件です。

履歴には message ID、宛先、試行番号、結果、失敗分類、SMTP stage/code、route/mode、
TLS必須フラグを保存します。メール本文、ヘッダ、MAIL FROM、SMTP AUTH、token、秘密鍵は保存せず、
相手サーバーの応答本文も保存しません。宛先自体は運用上必要なPIIなので、viewer権限と
Admin API のネットワーク境界を限定し、DBバックアップにも同じ保持期限を適用してください。

この履歴は OTEL trace、構造化ログ、Webhook を置き換えません。ホスト単位のTLS negotiationや
接続内訳は trace/TLS-RPTを参照し、履歴APIは再起動をまたぐ最低限の配送結果確認に使います。

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

## 宛先別配送の一時停止

[期限付き配送停止の設定・保存範囲](../configuration.md#宛先別配送の期限付き停止) を確認して有効化します。
復旧作業を始める前に、operator が `POST /api/v1/delivery/pauses` へ次の JSON を送ります。
最初は `dry_run: true`、確認後に `false` で実行してください。

```json
{
  "domain": "recipient.test",
  "reason": "DNS maintenance",
  "expires_at": "2026-10-21T01:00:00Z",
  "dry_run": true
}
```

`expires_at` は実行時点より後、30日以内の日時へ変更してください。
viewer は `GET /api/v1/delivery/pauses` で対象・理由・期限・作成主体を確認します。
停止中は suppression の追加や DLQ 移動を行わず、キュー容量と最古経過時間を監視します。
メッセージ寿命は停止中も進むため、長期作業では `max_retry_age` への到達にも注意してください。

復旧後は operator が `DELETE /api/v1/delivery/pauses/recipient.test?reason=restored&dry_run=1`
で確認し、`dry_run=1` を除いて再開します。反映は通常最大30秒です。
期限切れでも同じように再開し、既存の throttle と MX shaping を通します。
Redis 障害時は安全のため全宛先の新規 attempt を保留するので、まず状態ストアを復旧してください。
複数ノードでは停止 API を受け付けるノードと配送ノードが同じ Redis namespace を使うことを確認します。
