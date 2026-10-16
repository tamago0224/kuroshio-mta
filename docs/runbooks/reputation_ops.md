# Reputation Operations Runbook

Issue: #37, #278

## 目的

- IP / ドメインレピュテーション低下時に自動で送信制御する
- warm-up、bounce rate、complaint rate、TLS-RPT を可視化する

## 有効化

- `MTA_REPUTATION_START_DATE=2026-03-17`
- `MTA_REPUTATION_WARMUP_RULES=0:100,7:1000,14:5000`
- `MTA_REPUTATION_BOUNCE_THRESHOLD=0.05`
- `MTA_REPUTATION_COMPLAINT_THRESHOLD=0.001`
- `MTA_REPUTATION_MIN_SAMPLES=100`

## 動作

- warm-up 制限を超えると、その日の追加送信は一時抑止される
- permanent failure 比率が閾値以上になると送信を一時抑止する
- complaint 比率が閾値以上になると送信を一時抑止する
- TLS-RPT / complaint は管理API経由で記録できる
- 認証済み collector から送られたレポートを解析し、集計と重複判定を保存する

## 自動取り込み

既存の管理 API と同じ Bearer token を使い、`operator` 以上の権限で POST します。レポートの送信元確認・メール認証・転送は、信頼する外部 collector の責務です。これらの URL を、公開された無認証の TLS-RPT `rua` として直接使用しないでください。

| URL | 対応する本文 | 重複判定 |
| --- | --- | --- |
| `/api/v1/reputation/reports/tlsrpt` | RFC 8460 の JSON。`application/tlsrpt+json` / `application/json` / `application/tlsrpt+gzip` | `organization-name` と `report-id` |
| `/api/v1/reputation/reports/complaints` | RFC 5965 の `message/feedback-report`、または `multipart/report; report-type=feedback-report` の本文 | collector が設定する `Idempotency-Key` |

TLS-RPT の例:

```bash
curl --fail-with-body -X POST http://127.0.0.1:8080/api/v1/reputation/reports/tlsrpt \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H 'Content-Type: application/tlsrpt+json' \
  --data-binary @report.json
```

`report.json` には `organization-name`、`report-id`、`date-range`、`policies` が必要です。各 policy の `policy-domain` と summary の成功・失敗件数を集計します。件数は非負整数、期間は開始 < 終了、開始は受信時点から30日以内、終了は翌日までとします。1レポート100 policy までです。

complaint の例:

```bash
curl --fail-with-body -X POST http://127.0.0.1:8080/api/v1/reputation/reports/complaints \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H 'Content-Type: message/feedback-report' \
  -H 'Idempotency-Key: provider.example/report-123' \
  --data-binary @feedback.txt
```

`feedback.txt` は次のような feedback part です。メール全体をそのまま送る形式ではありません。

```text
Feedback-Type: abuse
Version: 1
User-Agent: trusted-collector/1
Original-Rcpt-To: rfc822; user@example.net
Incidents: 1
```

`Original-Rcpt-To` から宛先ドメインを取得します。`Incidents` を省略すると1件です。同じドメインの recipient が繰り返されても二重計上しません。`Idempotency-Key` は provider を含む一意な ID にし、再送時にも同じ値を使用してください。異なるレポートに同じ ID を再利用しないでください。

- HTTP 本文と gzip 展開後の上限はそれぞれ1 MiB
- multipart は10 part まで、feedback part は1つ。feedback part の転送符号化は未指定 / 7bit / 8bit
- 初回は `{"ok":true,"accepted":true,"duplicate":false}`、重複は `accepted:false` / `duplicate:true`
- 無認証は401、権限不足は403、不正なレポートは400、HTTP 本文の上限超過は413、保存失敗は500
- 500 の場合は成功として扱わず、同じ ID で再送する

## 保存と復旧

本体は `queue_dir/reputation.json` にドメイン別の累積件数、当日の warm-up 件数、重複判定用のハッシュを保存します。ファイルは0600で作成し、一時ファイルの同期・rename で置き換えます。レポート本文・recipient アドレス・生のレポート ID は保存しません。

- 通常の配送観測は5秒ごとに保存。異常終了時の RPO の目安は5秒ですが、最後に保存された後の観測は失われる可能性があります
- レポート取り込みと既存の手動記録 API は、保存後に成功応答する。保存失敗時は今回の変更を戻す
- 正常終了時は各ワーカーの停止後に最終保存する
- 重複判定は30日分を保持し、新規取り込み時に古い判定を除去する。累積件数は期限で削除せず、日別 warm-up 件数だけ UTC の日付変更でリセットする
- 起動時は保存済み件数から配送抑止を再評価する。破損・未対応バージョン・64 MiB を超える保存ファイルは、黙って初期化せず起動に失敗する
- 同じ保存ファイルを複数プロセスで共有しない。Kafka mode でも各ノード専用の `queue_dir` を使う

バックアップは MTA を停止してから `scripts/dr/backup_queue.sh` で queue 全体を取得してください。既存の backup / restore にこのファイルも含まれます。破損時はプロセスを停止したまま、正常なバックアップを空の復旧先へ戻し、`queue_dir` の所有者・権限を確認してから再起動します。ファイルの削除による初期化は累積件数と重複判定を失います。

## 可視化

- `/reputation` でドメインごとの状態を JSON 取得できる
- メトリクス:
  - `worker_reputation_block_total`
  - `reputation_warmup_limit_total`
  - `reputation_bounce_rate_block_total`
  - `reputation_complaint_rate_block_total`
- 取り込み結果は `reputation_report_ingest_total{result="accepted|duplicate|rejected|error"}` で確認できます。これはプロセス単位のカウンターで、再起動時にリセットします
- audit log の `reputation_report_ingested` / `reputation_report_rejected` で actor と結果を追跡できます。本文・アドレス・ID はログに出しません

## 収集操作

```bash
scripts/admin/kuroshio_admin.sh record-complaint gmail.com
scripts/admin/kuroshio_admin.sh record-tlsrpt gmail.com false
```

## 判定の見方

- `blocked=true`:
  そのドメイン向け配送は一時抑止中
- `block_reason=warmup_limit`:
  warm-up 日次上限に到達
- `block_reason=bounce_rate`:
  permanent failure 比率が閾値超過
- `block_reason=complaint_rate`:
  complaint 比率が閾値超過

## 制約

- 外部 collector からの取り込みが対象。メールボックスのポーリング、無認証の公開受信エンドポイント、provider 固有の送信元検証は提供しません
- JSON スナップショットは単一プロセス向け。ノード間でのレピュテーション集計の共有は対象外です
