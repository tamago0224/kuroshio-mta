# High-Volume SMTP Infrastructure — アーキテクチャ仕様書

**対象規模:** 日1億通  
**対応プロトコル:** SMTP · SPF · DKIM · DMARC · ARC · MTA-STS · DANE  
**優先事項:** スケーラビリティ · セキュリティ · コスト最適化 · 開発速度

---

## 目次

1. [システム全体像](#1-システム全体像)
2. [レイヤー別設計](#2-レイヤー別設計)
3. [セキュリティ技術詳細](#3-セキュリティ技術詳細)
4. [スケーリング戦略](#4-スケーリング戦略)
5. [キャパシティ計画](#5-キャパシティ計画)
6. [推奨技術スタック](#6-推奨技術スタック)
7. [実装チェックリスト](#7-実装チェックリスト)
8. [注意事項](#8-注意事項)

---

## 1. システム全体像

```
[Internet / Sending MTAs]
         ↓
┌─────────────────────────────────────────────┐
│  Layer 1: Ingress                           │
│  Anycast BGP → HAProxy/LB → Rate Limiter → DNSBL │
└─────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────┐
│  Layer 2: SMTP Processing                   │
│  SMTP Daemon → Auth Pipeline → TLS/DANE/MTA-STS → Milter │
└─────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────┐
│  Layer 3: Queue & Routing                   │
│  Kafka → Routing Engine → Priority Queue → Retry Manager │
└─────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────┐
│  Layer 4: Delivery                          │
│  Delivery Workers → Egress IP Pool → Bounce Processor → FBL │
└─────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────┐
│  Layer 5: Data & Observability              │
│  PostgreSQL / Redis / S3 → Prometheus / Jaeger → Grafana │
└─────────────────────────────────────────────┘
```

---

## 2. レイヤー別設計

### Layer 1: Ingress Layer

#### Anycast BGP
- BGP Anycastで同一IPアドレスを複数DCから広告し、最寄りのPoPへ自動ルーティング
- 障害時は別DCへ自動フェイルオーバー
- IPv4 + IPv6 デュアルスタック対応
- 最低3リージョン (Asia / US / EU) にPoP配置を推奨

#### HAProxy (L4 ロードバランサ)
- `mode tcp` (Layer 4) — SMTPプロトコルを透過
- TLS 1.2/1.3 終端
- ヘルスチェック: EHLO応答による死活監視
- `smtp_src` によるIPハッシュでスティッキーセッション
- maxconn: ワーカー1台あたり10,000接続を目標

#### Rate Limiter
- アルゴリズム: Sliding Window Counter (Redis)
- 制限軸: 送信元IP / EHLOドメイン / envelope-from
- IP単位: 100接続/分
- ドメイン単位: 1,000通/時
- 429応答 with Retry-After ヘッダ

#### DNSBL / Reputation
- 確認対象: Spamhaus ZEN, Barracuda BRBL, UCEProtect, SURBL
- 接続時にBLチェック → 5xxで即時拒否
- 並列DNS照会でレイテンシ最小化
- ローカルキャッシュ (TTL 300s) でDNS負荷削減

---

### Layer 2: SMTP Processing Layer

#### SMTP Daemon
- 推奨: **Haraka** (Node.js) — 高スループット・プラグインエコシステム
- 代替: **Postfix** — 実績・DANE対応済み
- 必要に応じて: **Golang自作** — 最大性能・完全制御
- 必須ESMTP拡張: STARTTLS, AUTH, PIPELINING, SIZE, 8BITMIME

#### Auth Pipeline (受信時処理フロー)
1. 接続受付 → SPFチェック (envelope-from)
2. DKIM署名検証 (Fromドメイン)
3. DMARCポリシー照合 (alignment確認)
4. ARCチェーン検証 (転送メールの場合)
5. Authentication-Resultsヘッダ付与
6. DMARCポリシーに従い配信 / 隔離 / 拒否

#### TLS / DANE / MTA-STS
- **優先順位:** DANE > MTA-STS > Opportunistic TLS
- DANE: DNSSECで保護されたTLSAレコードで証明書をピン留め
- MTA-STS: HTTPSでポリシーを公開・キャッシュ
- 詳細は [セキュリティ技術詳細](#3-セキュリティ技術詳細) を参照

#### Milter Pipeline
- Anti-spam: rspamd (高速・非同期・DKIM/DMARC/ARC全対応)
- Virus scan: ClamAV / 商用AV
- DLP: 個人情報・機密情報検出
- Header sanity check: 必須ヘッダ確認

---

### Layer 3: Queue & Routing Layer

#### Apache Kafka — トピック設計

| トピック | 用途 | Partitions |
|---|---|---|
| `mail.inbound` | 受信メール | 256 |
| `mail.outbound.transactional` | 高優先度送信 | 128 |
| `mail.outbound.bulk` | バルク送信 (レート制限あり) | 128 |
| `mail.retry` | 再送キュー | 64 |
| `mail.dlq` | Dead Letter Queue | 16 |
| `mail.events` | 配信イベント (delivered/bounced) | 64 |

**スループット計算:**
- 100M ÷ 86,400s = **1,157 msg/sec (平均)**
- ピーク係数 3x → **3,500 msg/sec**
- Kafka 1 partition = ~1,000 msg/sec → 合計512 partitions

#### Routing Engine
- 宛先ドメインのMXレコードを解決し優先度順にソート
- DANE TLSAレコード確認 (DNSSEC検証)
- MTA-STSポリシーキャッシュ確認
- ドメイン毎に最大接続数を設定 (例: Gmail 最大100並列接続)

#### Priority Queue
- Transactional専用IP + 専用Workers — SLA: 5秒以内
- Bulk: レート制限・別IPセグメント・時間帯制御
- KafkaトピックとConsumer Groupを完全分離

#### Retry Manager
- **Exponential Backoff スケジュール:**

| 試行回数 | 待機時間 |
|---|---|
| 1回目 | 5分後 |
| 2回目 | 30分後 |
| 3回目 | 2時間後 |
| 4回目 | 6時間後 |
| 5回目以降 | 24時間毎 |
| 最大保持 | 5日間 (RFC 5321) |

- 4xx → ソフトバウンス → リトライ
- 5xx → ハードバウンス → DSN即時生成 (RFC 3464)

---

### Layer 4: Delivery Layer

#### Delivery Workers
- Kubernetes HPAで自動スケール
- Kafkaのlag監視でWorker数を調整
- フルスケール時: 400〜800 Workers
- 1 Workerの処理能力: ~2,500 msg/hour、10〜50並列SMTP接続

#### Egress IP Pool

**IP Pool分離:**
- Transactional専用IP — 高レピュテーション維持
- Bulk送信IP — 別セグメントで隔離

**IP Warmupスケジュール (新規IP):**

| 期間 | 送信上限 |
|---|---|
| Day 1 | 200通 |
| Day 3 | 500通 |
| Week 2 | 2,000通 |
| Month 1 | 10,000通 |
| Month 3 | フル稼働 |

**モニタリング:**
- Gmail Postmaster Tools — 日次確認
- Microsoft SNDS — 週次確認
- DNSBL自動チェック — 1時間毎

#### Bounce Processor
- Hard Bounce (5xx) → 即時抑制リストへ追加
- Soft Bounce (4xx) → Retry Managerへ
- DSNパース (RFC 3464) → DB記録 → 抑制リスト更新 → Webhook通知

#### FBL / SNDS
- Microsoft JMRP/SNDS、Yahoo FBL、SpamCopからの苦情を受信
- ARFフォーマットパース → 対象アドレスを抑制リストへ追加
- **苦情率 > 0.1% → アラート・送信一時停止**

---

### Layer 5: Data & Observability Layer

#### ストレージ

| 用途 | 技術 | 内容 |
|---|---|---|
| メタデータ | PostgreSQL 16 | 送信ログ・設定・抑制リスト |
| キャッシュ | Redis Cluster | Rate limit・MTA-STSキャッシュ・DNS |
| オブジェクト | S3 / MinIO | メール本文・添付・長期ログ |
| 全文検索 | Elasticsearch | ログ検索 |

#### 観測性スタック
- メトリクス: Prometheus + Grafana
- トレーシング: Jaeger / Tempo
- ログ集約: Loki / ELK Stack
- アラート: PagerDuty

**主要アラート閾値:**

| メトリクス | 閾値 | アクション |
|---|---|---|
| 配信率 | < 95% | PagerDuty通知 |
| バウンス率 | > 5% | 即時対応 |
| 苦情率 | > 0.1% | 送信停止 |
| IP DNSBL登録 | 検知次第 | 緊急アラート |
| キューdepth | > 10M | スケールアウト |

---

## 3. セキュリティ技術詳細

### SPF (RFC 7208)

**役割:** DNSのTXTレコードで送信許可IPを宣言し、受信側がenvelope-fromドメインと照合する。

**実装ポイント:**
- 10 DNS lookupルールに注意 (include展開で容易に上限超過)
- 複数送信IPは `ip4:` / `ip6:` 機構で明示列挙
- `~all` (SoftFail) から始め、段階的に `-all` (HardFail) へ移行
- 設定例:
  ```
  v=spf1 ip4:203.0.113.0/24 ip6:2001:db8::/32 include:sendgrid.net ~all
  ```

---

### DKIM (RFC 6376 + RFC 8463)

**役割:** 秘密鍵でメール本文・ヘッダに署名し、受信側がDNS公開鍵で検証する。

**実装ポイント:**
- **Ed25519** (`k=ed25519`) を優先、RSA-2048も並用
- 署名対象ヘッダに必ず含めるもの: `From`, `Subject`, `Date`, `To`
- `l=` タグ (body length) は使用禁止 → 改ざんリスク
- セレクタのローテーション: 月次で鍵交換を自動化
- 秘密鍵の管理: HSM または AWS KMS で保護

**DNSレコード例:**
```
mail._domainkey.example.com  TXT  "v=DKIM1; k=ed25519; p=<公開鍵>"
```

---

### DMARC (RFC 7489)

**役割:** SPF/DKIM両方を統合するポリシー層。認証失敗時の処理方針を宣言する。

**ポリシー移行フロー:** `p=none` → `p=quarantine` → `p=reject`

**実装ポイント:**
- `rua` タグで集計レポート受信エンドポイントを設定 (XML形式の日次レポート)
- `ruf` タグでフォレンジックレポート受信 (失敗メールのサンプル)
- identifier alignment: strict vs relaxed を要件に応じて設定
- サブドメイン用 `sp=` ポリシーも設定
- **Gmail等への一括送信: `p=quarantine` 以上が2024年以降必須**

**DNSレコード例:**
```
_dmarc.example.com  TXT  "v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com; ruf=mailto:dmarc-failures@example.com; pct=100"
```

---

### ARC (RFC 8617)

**役割:** メーリングリスト・転送でSPF/DKIMが破損する問題を補完する認証チェーン。

**仕組み:** 各中継MTAが `ARC-Seal` / `ARC-Message-Signature` / `ARC-Authentication-Results` の3ヘッダを付与し、受信MTAがチェーン全体を検証する。

**実装ポイント:**
- OpenARCライブラリを活用 (Postfix integration)
- `i=` インスタンスタグを適切にインクリメント
- `ARC-Authentication-Results` ヘッダを正確に引き継ぐ
- メーリングリストシステムには**必須実装**

---

### MTA-STS (RFC 8461)

**役割:** TLS強制ポリシーをHTTPS経由で公開し、接続前に相手MTAへTLS必須を伝える。

#### 受信側実装 (自ドメインを保護)

**Step 1 — DNSレコード:**
```
_mta-sts.example.com  TXT  "v=STSv1; id=20240201T120000"
```
> `id` を変更するとクライアントにポリシー再取得を促す。

**Step 2 — ポリシーファイルをHTTPSで公開:**
```
URL: https://mta-sts.example.com/.well-known/mta-sts.txt

version: STSv1
mode: enforce
mx: mail.example.com
mx: *.example.com
max_age: 604800
```
> NginxやS3静的ホスティングで十分。

**移行フロー:** `mode: testing` → 問題なければ `mode: enforce`

#### 送信側実装 (相手のポリシーを遵守)

| SMTPデーモン | 実装方法 |
|---|---|
| Postfix | `postfix-mta-sts-resolver` デーモン連携 |
| Haraka | `haraka-plugin-mta-sts` プラグイン |
| Golang自作 | `emersion/go-smtp` + MTA-STSライブラリ |

> MTA-STSはMilterでは実現**不可**。Milterはメール受付後に動くため、TLS接続フェーズには介入できない。

#### TLSRPT (RFC 8460) — セットで必須設定

```
_smtp._tls.example.com  TXT  "v=TLSRPTv1; rua=mailto:tls-reports@example.com"
```

相手MTAがTLS接続に失敗した場合、日次レポートを受信できる。MTA-STS設定ミスの検知手段として**必須**。

---

### DANE (RFC 7672)

**役割:** DNSSECで保護されたTLSAレコードで証明書をピン留めし、CA非依存の証明書検証を実現。

**前提条件:** DNSSECが有効なこと (親ゾーンからのチェーン確立が必須)

**実装ポイント:**
- TLSA推奨タイプ: `3 1 1` (DANE-EE, SPKI, SHA-256)
- Postfix: `smtp_tls_security_level = dane` で有効化
- TLSAレコードと証明書の**同期更新**が重要 (証明書更新前にTLSAを先に更新)
- 未対応MXへのフォールバック戦略を設計

**TLSAレコード例:**
```
_25._tcp.mail.example.com  TLSA  3 1 1 <証明書公開鍵のSHA-256ハッシュ>
```

**優先順位:** DANE > MTA-STS > Opportunistic TLS

---

## 4. スケーリング戦略

### フェーズ別スケールアップ計画

| フェーズ | 送信量/日 | アーキテクチャ | SMTP Workers | Kafka Partitions | 送信IP | 月次コスト目安 |
|---|---|---|---|---|---|---|
| Phase 1 MVP | 1M | モノリス / 単一DC | 4〜8 | 16 | 4〜8 | ~$500 |
| Phase 2 Growth | 10M | マイクロサービス分離 | 20〜50 | 64 | 20〜50 | ~$3,000 |
| Phase 3 Scale | 50M | マルチリージョン / Anycast | 100〜200 | 256 | 100+ | ~$12,000 |
| Phase 4 Full | 100M | グローバル分散 / 3+ DC | 400〜800 | 512+ | 200〜500 | ~$25,000 |

### IP Reputation 管理戦略

- Transactional専用IPとBulk送信IPを**完全分離**
- 新規IPはWarmupスケジュールに従い段階的に増量
- IP毎の送信上限目安: **500,000通/日**

---

## 5. キャパシティ計画

### 数値設計

| 指標 | 値 | 算出根拠 |
|---|---|---|
| 平均スループット | 1,157 msg/sec | 100M ÷ 86,400s |
| ピークスループット | 3,500 msg/sec | 平均 × 3 (朝9時・キャンペーン) |
| 必要Kafka Partitions | 512 | 3,500 msg/sec ÷ ~7 msg/sec/partition |
| Delivery Workers (フル) | 400〜800 | 2,500 msg/hour/worker |
| 送信IPアドレス数 | 200〜500 | 500K通/日/IP |
| ストレージ | 10TB+/月 | ログ・メタデータ・DMARCレポート |

---

## 6. 推奨技術スタック

```yaml
smtp_daemon:
  primary:   Haraka (Node.js)      # 高スループット・プラグイン拡張
  fallback:  Postfix                # 実績・DANE対応
  custom:    Golang SMTP server     # 最大性能が必要な場合

message_queue:
  broker:    Apache Kafka 3.x       # 100M/day throughput に最適
  streams:   Kafka Streams          # リアルタイム処理
  dlq:       Separate DLQ topic     # 失敗メッセージ分離

auth_libraries:
  spf:       pyspf / golang-spf
  dkim:      OpenDKIM / go-dkim     # Ed25519対応確認
  dmarc:     OpenDMARC
  arc:       OpenARC
  dane:      Postfix built-in       # smtp_tls_security_level=dane
  mta-sts:   postfix-mta-sts-resolver

database:
  metadata:  PostgreSQL 16          # 送信ログ・設定
  cache:     Redis Cluster          # Rate limit・MTA-STSキャッシュ
  objects:   S3 / MinIO             # メール本文・添付
  search:    Elasticsearch          # ログ全文検索

observability:
  metrics:   Prometheus + Grafana
  tracing:   Jaeger / Tempo
  logging:   Loki / ELK Stack
  alerting:  PagerDuty

infrastructure:
  cloud:     AWS / GCP              # ポート25がデフォルト閉塞 → 申請必要
  on-prem:   自社DC推奨             # IP評判管理のため
  dns:       Route53 + DNSSEC       # DANE前提
  bgp:       Anycast (BIRD)         # グローバルPoP
  container: Kubernetes + HPA       # 自動スケール
```

---

## 7. 実装チェックリスト

### セキュリティ認証設定

- [ ] **SPF レコード設定** — DNS TXTレコード公開・10 lookup制限確認
- [ ] **DKIM 署名実装** — Ed25519 + RSA2048並用・セレクター設計
- [ ] **DKIM 鍵ローテーション自動化** — 月次ローテーション・KMS連携
- [ ] **DMARC ポリシー設定 (none→)** — rua/ruf レポートエンドポイント構築
- [ ] **DMARC レポートパーサー** — XML集計レポート自動処理・可視化
- [ ] **ARC 署名実装** — OpenARC導入・転送時チェーン維持
- [ ] **MTA-STS ポリシーファイル公開** — HTTPS `.well-known/mta-sts.txt`
- [ ] **TLSRPT 設定** — TLS配信失敗レポート受信設定
- [ ] **DANE / DNSSEC 設定** — TLSA RR (3 1 1) + DNSSEC署名
- [ ] **TLS 1.2+ 強制** — TLS 1.0/1.1無効化・強力なcipher優先

### インフラ・運用設定

- [ ] **PTR / rDNS 設定** — 送信IP全てにFQDNのrDNSレコード
- [ ] **HELO/EHLO ホスト名設定** — 送信IPと一致するFQDN
- [ ] **IP Warming スケジュール** — 新規IP段階的増量計画策定
- [ ] **Transactional / Bulk IP分離** — サービス種別でIPセグメント分け
- [ ] **DNSBL 監視設定** — 主要ブラックリスト登録を自動検知 (1時間毎)
- [ ] **Gmail Postmaster Tools 登録** — 送信ドメイン登録・Reputation監視
- [ ] **Microsoft SNDS / JMRP 登録** — MicrosoftへのIP登録・FBL設定
- [ ] **バウンス処理実装** — Hard/Softバウンス分類・自動抑制
- [ ] **List-Unsubscribe ヘッダ実装** — RFC 8058 One-Click Unsubscribe
- [ ] **配信率・遅延モニタリング** — Grafanaダッシュボード・PagerDutyアラート

---

## 8. 注意事項

### クラウド利用時

AWS / GCP / Azureはデフォルトで**ポート25 (SMTP)をブロック**しています。大規模送信には申請・制限解除が必要、またはSES/SendGridなどのリレーサービスを経由する必要があります。

IP評判管理の観点から、本番環境では**自社保有IPアドレスを使った自社DC or 専用サーバ**を強く推奨します。

### Gmail / Yahoo 一括送信要件 (2024年〜)

Googleおよびびお送信ドメインには以下が必須:

- `p=quarantine` 以上のDMARCポリシー
- DKIM署名の実装
- List-Unsubscribe One-Click (RFC 8058)
- スパム率 < 0.1%

### 参照RFC一覧

| 技術 | RFC |
|---|---|
| SMTP | RFC 5321 |
| SPF | RFC 7208 |
| DKIM | RFC 6376, RFC 8463 (Ed25519) |
| DMARC | RFC 7489 |
| ARC | RFC 8617 |
| MTA-STS | RFC 8461 |
| TLSRPT | RFC 8460 |
| DANE for SMTP | RFC 7672 |
| DSN | RFC 3461, RFC 3464 |
| List-Unsubscribe | RFC 8058 |