# SLO Runbook: Queue Backlog

1. `smtp_queued_messages_total - worker_ack_sent_total - worker_mark_failed_total` を確認
2. worker 稼働数 (`MTA_WORKER_COUNT`) とドメイン制御設定を見直す
3. 外部依存障害（DNS, MX, TLS）を確認し、影響ドメインを切り分ける
4. backlog 解消後に再発防止（ドメインルール/再送ポリシー）を更新
