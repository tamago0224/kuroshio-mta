# SLO Runbook: Queue Backlog

1. `smtp_queued_messages_total - worker_ack_sent_total - worker_mark_failed_total` を確認
2. worker 稼働数 (`MTA_WORKER_COUNT`) とドメイン制御設定を見直す
3. 外部依存障害（DNS, MX, TLS）を確認し、影響ドメインを切り分ける
4. backlog 解消後に再発防止（ドメインルール/再送ポリシー）を更新

## 容量制限による受信停止

`queue_max_messages` / `queue_max_bytes` を high watermark として使います。
拒否後は、両方の使用量が `queue_low_watermark_percent`（未指定・0 は80%）以下になり、
空き容量条件も満たすと、次の投入時に再開します。上限ちょうどまで保存可能です。
単通でバイト上限を超えるメールも451となるため、`max_message_bytes` と追加ヘッダ分を考慮して設定してください。

1. `/metrics` の `queue_pending_messages`、`queue_pending_bytes`、`queue_capacity_paused` を確認します。
   これらは容量制限有効時の起動時・キュー操作後のスナップショットです。
2. `queue_capacity_rejected_total` の増加と SMTP の451応答を確認します。
   `queue_disk_available_bytes` は直近の投入判定時の空き容量です。
3. 配送先障害を解消し、再送の進行を確認します。送信済み・DLQ・poison の保存容量は
   未配送上限に含まれないため、保持期限と実際のディスク容量も確認します。
4. 低水位以下で再投入し、`queue_capacity_resumed_total` の増加と250応答を確認します。
   `queue_capacity_scan_errors_total` が増える場合はファイル権限・破損を調べます。
   破損ファイルがある間は投入を失敗させ、ワーカーの poison 隔離または既存の復旧手順で解消します。

計測対象は `mail.inbound`、`mail.retry`、旧 `pending` 内の JSON です。投入・状態遷移のたびに
保存データを読み直すため、再起動後も復元でき、再送は容量を解放せず、sent / DLQ への遷移は解放します。
管理 API の sent / DLQ 再投入にも同じ上限を適用し、拒否時は元ファイルを保持します。
管理 API の既存エラー応答は変更していません。

初期実装は全件スキャンで、件数に比例する I/O が発生します。大規模キューでは投入遅延を測定してください。
保証範囲は、単一プロセスが専有するローカルファイルシステム上のキューディレクトリです。
排他ロックは容量制限を有効にしたプロセス同士に適用します。制限無効の別プロセスや外部ツールから同じキューを変更しないでください。
共有/NFS ディレクトリや複数ノードでの合算上限はサポートしません。
空き容量検査は受信 JSON の保存量を差し引きますが、他プロセスの書き込みや配送時の一時コピー、
ファイルシステムのブロック・メタデータ割当までは予約しません。十分な余裕を設定し、保存失敗時は451で再送を促します。

Kafka ではローカルディスク使用量と broker の inbound / retry backlog は異なります。
consumer lag の集計には遅延があり、並行 producer を含めた厳密な予約を提供できないため、
容量上限または空き容量制限を設定すると起動を拒否します。broker の容量・retention・lag を別途監視してください。
