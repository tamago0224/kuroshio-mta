# SLO Runbook: Delivery Success Rate

1. `worker_temporary_failure_total` と `worker_permanent_bounce_total` の増加ドメインを確認
2. DNS/MX/TLS (DANE, MTA-STS) 障害有無を確認
3. 特定ドメイン障害の場合は `MTA_DOMAIN_MAX_CONCURRENT_RULES` を一時的に絞る
4. 復旧後は通常値へ戻し、事後分析を記録
