# SLO Runbook: Retry Rate

1. `worker_temporary_failure_total` 増加時刻と対象ドメインを特定
2. 4xx 応答の主要要因（接続拒否/レート制限/TLS失敗）をログで確認
3. 必要に応じて `MTA_DOMAIN_PENALTY_MAX` と並行度を調整
4. 改善後に再試行遅延とDLQ流入率を確認
