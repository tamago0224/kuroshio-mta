---
layout: home

hero:
  name: kuroshio-mta
  text: Go で実装した MTA の設計と運用ドキュメント
  tagline: 起動から運用、Observability、RFC 対応、設計方針までを一つの流れで辿れるドキュメントサイトです。
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
  - icon: 🚀
    title: Getting Started
    details: サンプル設定の作成から `go run ./cmd/kuroshio -config ./config.yaml` での起動までを最短手順で追えます。
  - icon: 🧪
    title: 機能チュートリアル
    details: 最小メールフロー、メール認証、TLS 配送、Rate Limit、Admin API など主要機能を `docker compose` で順に試せます。
  - icon: 📈
    title: 設定と運用
    details: YAML ベースの設定、Observability Stack、Kafka Queue モード、Admin API や SLO runbook までまとめて参照できます。
  - icon: 📚
    title: RFC 対応状況
    details: SMTP、STARTTLS、SPF、DKIM、DMARC、ARC、MTA-STS、DANE などの実装範囲とギャップを追えます。
  - icon: 🧭
    title: 設計メモ
    details: HA 構成や正規化方針など、実装の意図を後から確認しやすい形で残しています。

---

## 目的別の入口

| いまの目的 | まず開くページ | 区分 |
| --- | --- | --- |
| 最短で起動したい | [Getting Started](/getting-started) | 現行 |
| 設定値を確認したい | [Configuration](/configuration) | 現行 |
| 手元で機能を試したい | [Tutorials](/tutorials/) | 現行 |
| 障害対応手順を見たい | [Runbooks 一覧](/runbooks/) | 現行 |
| 設計意図や将来方針を知りたい | [Architecture Docs](/architecture/normalization_policy) | 設計 |

## 3つの定番ルート

1. 初回セットアップ: [Getting Started](/getting-started) → [Configuration](/configuration) → [最小メールフローを試す](/tutorials/basic-mail-flow)
2. 運用対応: [Runbooks 一覧](/runbooks/) → [Observability](/observability) → [SLO Runbooks](/runbooks/slo_delivery)
3. 設計把握: [正規化方針](/architecture/normalization_policy) → [Domain Throttle Externalization](/architecture/domain_throttle_externalization) → [SMTP AUTH Modern Auth Direction](/architecture/smtp_auth_modern_auth)

## 読み分けガイド（現行仕様 / 設計メモ）

この docs には、**いま使うための手順**と、**将来方針を含む設計メモ**が混在しています。  
用途に応じて、次のように読むのがおすすめです。

### 現行仕様・運用（まずこちら）

- [Getting Started](/getting-started)
- [Configuration](/configuration)
- [Tutorials](/tutorials/)
- [Runbooks 一覧](/runbooks/)
- [Observability](/observability)
- [Rate Limit](/rate_limit)
- [Kafka Queue モード](/kafka_queue_mode)
- [S3 Spool Backend](/s3_spool_backend)
- [Observability Signals](/observability_signals)

### 設計メモ・将来方針（意図やロードマップ）

- [Admin Auth DB Direction](/architecture/admin_auth_db_direction)
- [SMTP AUTH Modern Auth Direction](/architecture/smtp_auth_modern_auth)
- [Domain Throttle Externalization](/architecture/domain_throttle_externalization)
- [正規化方針](/architecture/normalization_policy)
- [HA Reference](/architecture/ha_reference)
- [RFC ギャップメモ](/rfc_4954_gap)

::: tip
`Direction` / `Policy` / `Gap` を含むページは、将来拡張や非目標の記述を含みます。  
直近の運用判断では、まず `Getting Started` / `Configuration` / `Runbooks` を優先してください。
:::

## 読み分けガイド（現行仕様 / 設計メモ）

この docs には、**いま使うための手順**と、**将来方針を含む設計メモ**が混在しています。  
用途に応じて、次のように読むのがおすすめです。

### 現行仕様・運用（まずこちら）

- [Getting Started](/getting-started)
- [Configuration](/configuration)
- [Tutorials](/tutorials/)
- [Runbooks](/runbooks/submission_auth)
- [Observability](/observability)
- [Rate Limit](/rate_limit)
- [Kafka Queue モード](/kafka_queue_mode)
- [S3 Spool Backend](/s3_spool_backend)

### 設計メモ・将来方針（意図やロードマップ）

- [Admin Auth DB Direction](/architecture/admin_auth_db_direction)
- [SMTP AUTH Modern Auth Direction](/architecture/smtp_auth_modern_auth)
- [Domain Throttle Externalization](/architecture/domain_throttle_externalization)
- [正規化方針](/architecture/normalization_policy)
- [HA Reference](/architecture/ha_reference)
- [RFC ギャップメモ](/rfc_4954_gap)（「実装範囲外」「判断メモ」を含みます）

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
