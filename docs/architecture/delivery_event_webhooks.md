# Delivery Event Webhooks

## Schema と配信保証

有効時は `accepted`、`delivered`、`temporary_failure`、`permanent_failure`、`delivery_unknown` を `kuroshio.delivery-event.v1` JSON として通知します。

```json
{
  "schema_version": "kuroshio.delivery-event.v1",
  "event_id": "8a2f...",
  "message_id": "message-id",
  "recipient_id": "sha256:...",
  "result": "delivered",
  "occurred_at": "2026-10-23T12:00:00Z",
  "attempt": 1,
  "category": "transport"
}
```

同じ event は再送時も同じ `event_id` を使います。受信側は `event_id` を一意キーとして冪等処理してください。配送結果 event はメール queue の sent/retry/DLQ 更新より先に、`queue_dir/events/pending` へ一時ファイル、fsync、atomic rename、directory fsync の順で保存します。このため配送成功直後に停止しても、保存済み event は再起動後に再送されます。Webhook 成功とローカル削除の間で停止した場合は同じ event が再送されるため、保証は exactly-once ではなく at-least-once です。

`accepted` event はメールの永続化成功後に outbox へ保存します。この2操作は単一 transaction ではないため、その間のプロセス/ホスト障害では受理済みメールの accepted event が欠落し得ます。配送結果 event は後続処理で独立して生成されます。

## HTTP と再送

- 2xx: 配信成功として pending から削除
- 429: `Retry-After` の秒指定と exponential backoff の長い方で再送
- 5xx、timeout、接続/TLS失敗: exponential backoff で再送
- 401/403、その他4xx、redirect: 設定または認証の恒久障害として直ちに DLQ
- `max_attempts` 到達: DLQ

外部呼び出しには timeout を設定し、環境変数 proxy と redirect を使いません。通常は HTTPS と OS trust store で証明書を検証します。平文 HTTP は `allow_http_loopback: true` の literal loopback IP に限ります。

## 署名・secret rotation・PII

request body の HMAC-SHA256 hex を `X-Kuroshio-Signature: sha256=<hex>`、同じ識別子を `X-Kuroshio-Event-ID` で送ります。受信側は body の生 bytes に対して HMAC を計算し、constant-time 比較してください。secret は `secret_file` から起動時だけ読み、ログ・event・エラーへ出しません。

rotation は受信側で旧/新 secret の両方を一時的に受理し、secret file を更新して MTA を再起動し、旧 secret の受理を止める順で行います。pending event は dispatch 時の現行 secret で署名され、event_id は変わりません。

recipient の生アドレス、本文、header、送信者、認証情報は event に含めません。`recipient_id` は trim・小文字化した envelope recipient の SHA-256 であり、同じアドレスの相関は可能です。message ID も外部連携識別子として個人データになり得るため、endpoint は管理者の信頼境界内に置いてください。

## 容量・保持・監視

pending が `max_pending` に達した場合、メール受理・配送は停止せず event を破棄し、`event_webhook_dropped_total` とエラーログで通知します。これは Webhook 障害がメール配送を無制限に停止することを避ける意図的な境界です。pending と DLQ は `retention` 経過後に削除します。

主な指標は `event_webhook_enqueued_total`、`event_webhook_delivered_total`、`event_webhook_retried_total`、`event_webhook_dlq_total`、`event_webhook_dropped_total`、`event_webhook_corrupt_total`、`event_webhook_store_errors_total`、`event_webhook_pending` です。不正・破損した永続 record は他の event を停止させず `.corrupt` として DLQ へ移します。DLQ は `queue_dir/events/dlq` で調査します。secret や payload 全体をログへ出さないでください。
