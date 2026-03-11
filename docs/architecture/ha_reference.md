# Orinoco MTA HA Reference Architecture

## 目標

- RTO: 5分以内（単一AZ障害）
- RPO: 1分以内（キューレプリケーション前提）
- 単一コンポーネント障害で配送停止しない

## 構成（マルチAZ, Active-Active）

```text
                     +-------------------------------+
                     |      Global DNS / LB          |
                     +---------------+---------------+
                                     |
                     +---------------+---------------+
                     |                               |
             +-------v-------+               +-------v-------+
             |   AZ-A Ingress|               |   AZ-B Ingress|
             |  SMTP/Submit  |               |  SMTP/Submit  |
             +-------+-------+               +-------+-------+
                     |                               |
             +-------v-------+               +-------v-------+
             |   Queue Broker|<--replicate-->|   Queue Broker|
             |  (cluster A)  |               |  (cluster B)  |
             +-------+-------+               +-------+-------+
                     |                               |
             +-------v-------+               +-------v-------+
             | Worker Pool A |               | Worker Pool B |
             +-------+-------+               +-------+-------+
                     |                               |
                     +---------------+---------------+
                                     |
                           +---------v---------+
                           | External MX hosts |
                           +-------------------+

        Observability(Prometheus/Grafana/Alertmanager) は各AZに配置し相互監視
```

## Active-Active / Active-Standby 比較

| 項目 | Active-Active | Active-Standby |
| --- | --- | --- |
| 平常時コスト | 高い | 低い |
| 障害時RTO | 低い（秒〜分） | 高い（分〜十数分） |
| 運用複雑度 | 高い | 中 |
| 推奨用途 | 高スループット本番 | 小規模/段階導入 |

採用方針:
- 本番基準は Active-Active（本ドキュメントの前提）
- コスト制約時は Active-Standby を暫定採用し、段階的にActive-Activeへ移行

## フェイルオーバー判定条件

- Ingress:
  - `/healthz` の連続失敗（3回/30秒）
  - 5xx率 > 20% が5分継続
- Queue:
  - broker quorum 喪失
  - produce/consume エラー率 > 30% が5分継続
- Worker:
  - `worker_delivery_success_total` 低下 + `worker_temporary_failure_total` 急増
- DNS:
  - authoritative DNS 応答失敗、または解決遅延しきい値超過

## フェイルオーバー手順

1. Alertmanager で重大度 `page` 通知を確認
2. 影響スコープ（AZ単位/全体）を判定
3. DNS/LB 重みを健全AZへ寄せる（または不健全AZを切離し）
4. Queue 健全性確認（under-replicated partitions, lag）
5. Worker 容量を健全AZで一時増強
6. `/slo` と `/metrics` で復旧判定（SLO breach 解消）
7. 事後に元構成へ段階復帰

## 障害注入テスト計画

- AZ断（Ingress/Worker停止）
- Broker断（片系 broker kill）
- DNS障害（名前解決失敗/遅延）

各シナリオの実行は `scripts/chaos/run_ha_drill.sh` を参照。
