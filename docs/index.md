---
layout: home

hero:
  name: kuroshio-mta
  text: Go で実装した MTA の設計と運用ドキュメント
  tagline: Getting Started、compose で試せる機能チュートリアル、RFC 対応、運用 runbook、アーキテクチャ方針をブラウザでたどれるように整理したドキュメントサイトです。
  actions:
    - theme: brand
      text: Getting Started を見る
      link: /getting-started
    - theme: alt
      text: Tutorials を見る
      link: /tutorials/
    - theme: alt
      text: 設定ガイドを見る
      link: /configuration
    - theme: alt
      text: GitHub Repository
      link: https://github.com/tamago0224/kuroshio-mta

features:
  - title: Getting Started
    details: サンプル設定の作成から `go run ./cmd/kuroshio -config ./config.yaml` での起動までを最短手順で追えます。
  - title: 機能チュートリアル
    details: 最小メールフロー、メール認証、TLS 配送、Rate Limit、Admin API など主要機能を `docker compose` で順に試せます。
  - title: 設定と運用
    details: YAML ベースの設定、Observability Stack、Rate Limit、Kafka Queue モード、Admin API や SLO runbook までまとめて参照できます。
  - title: RFC 対応状況
    details: SMTP、STARTTLS、SPF、DKIM、DMARC、ARC、MTA-STS、DANE などの実装範囲とギャップを追えます。
  - title: 設計メモ
    details: HA 構成や正規化方針など、実装の意図を後から確認しやすい形で残しています。

---

## よく使うページ

- [Getting Started](/getting-started)
- [Tutorials](/tutorials/)
- [最小メールフローを試す](/tutorials/basic-mail-flow)
- [メール認証を試す](/tutorials/mail-auth)
- [TLS 配送ポリシーを試す](/tutorials/tls-policy)
- [Rate Limit を試す](/tutorials/rate-limit)
- [Admin API を試す](/tutorials/admin-operations)
- [設定ガイド](/configuration)
- [Observability Stack](/observability_stack)
- [Observability Signals](/observability_signals)
- [Observability](/observability)
- [S3 Spool Backend](/s3_spool_backend)
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
