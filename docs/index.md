---
layout: home

hero:
  name: kuroshio-mta
  text: Go で実装した MTA の設計と運用ドキュメント
  tagline: 設定、RFC 対応、運用 runbook、アーキテクチャ方針をブラウザでたどれるように整理したドキュメントサイトです。
  actions:
    - theme: brand
      text: 設定ガイドを見る
      link: /configuration
    - theme: alt
      text: 正規化方針を見る
      link: /architecture/normalization_policy
    - theme: alt
      text: GitHub Repository
      link: https://github.com/tamago0224/kuroshio-mta

features:
  - title: 設定と運用
    details: YAML ベースの設定、Rate Limit、Kafka Queue モード、Admin API や SLO runbook までまとめて参照できます。
  - title: RFC 対応状況
    details: SMTP、STARTTLS、SPF、DKIM、DMARC、ARC、MTA-STS、DANE などの実装範囲とギャップを追えます。
  - title: 設計メモ
    details: HA 構成や正規化方針など、実装の意図を後から確認しやすい形で残しています。

---

## よく使うページ

- [設定ガイド](/configuration)
- [Rate Limit](/rate_limit)
- [Kafka Queue モード](/kafka_queue_mode)
- [正規化方針](/architecture/normalization_policy)
- [HA Reference](/architecture/ha_reference)

## RFC ギャップメモ

- [RFC 1870: SMTP SIZE](/rfc_1870_gap)
- [RFC 3207: SMTP STARTTLS](/rfc_3207_gap)
- [RFC 3464: DSN](/rfc_3464_gap)
- [RFC 4954: SMTP AUTH](/rfc_4954_gap)
- [RFC 6152: 8BITMIME](/rfc_6152_gap)
- [RFC 6376: DKIM](/rfc_6376_gap)
- [RFC 6409: Message Submission](/rfc_6409_gap)
- [RFC 7208: SPF](/rfc_7208_gap)
- [RFC 7489: DMARC](/rfc_7489_gap)
- [RFC 7672: DANE for SMTP](/rfc_7672_gap)
- [RFC 8461: MTA-STS](/rfc_8461_gap)
- [RFC 8617: ARC](/rfc_8617_gap)
