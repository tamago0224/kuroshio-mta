# kuroshio-mta

`kuroshio-mta` は Go で実装した MTA（Mail Transfer Agent）です。

黒潮は日本近海を流れる世界有数の海流で、
大量のメールを力強く安定して運ぶ MTA をイメージして命名しています。

## 現在の実装範囲

- SMTP受信サーバー（自前パーサー）
- Submission サーバー（SMTP AUTH PLAIN / LOGIN）
- ローカル永続キュー（`var/queue`）
- MX解決による配送先ルーティング
- SMTP配送ワーカー（再送バックオフ付き）
- STARTTLS対応先への送信時TLS昇格
- DKIM / ARC 送信署名、SPF / DKIM / DMARC / ARC 受信評価

## RFC対応状況（現時点）

注記:
- `対応済み（実装範囲内）` は、現行の Orinoco MTA が対象としている機能範囲では実装済みであることを示します。
- 周辺RFCとの完全な相互運用や、未採用オプションまで含む全面実装を意味するものではありません。

| RFC | 技術 | 対応状況 | 補足 |
| --- | --- | --- | --- |
| RFC 5321 | SMTP | 対応済み（実装範囲内） | `EHLO/HELO`, `MAIL FROM`, `RCPT TO`, `DATA`, `RSET`, `NOOP`, `QUIT`, `HELP`, `VRFY` を実装。`EXPN` は `502` 応答、`Received:` トレースヘッダ、`postmaster` 宛特例、主要な構文/状態遷移/行長制限のコンフォーマンステストを整備。拡張は各RFCで管理 |
| RFC 3207 | SMTP STARTTLS | 対応済み（実装範囲内） | 受信側/送信側で STARTTLS 昇格、TLS 後の再 EHLO、セッション再初期化を実装。詳細は [rfc_3207_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_3207_gap.md) |
| RFC 1870 | SMTP SIZE | 対応済み（実装範囲内） | EHLO での `SIZE` 広告、`MAIL FROM SIZE=`、上限超過拒否、境界値テストを実装。詳細は [rfc_1870_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_1870_gap.md) |
| RFC 6152 | 8BITMIME | 対応済み（実装範囲内） | EHLO での `8BITMIME` 広告、`BODY=8BITMIME` / `BODY=7BIT`、8bit 本文の受理/拒否を実装。詳細は [rfc_6152_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_6152_gap.md) |
| RFC 4954 | SMTP AUTH | 一部対応 | Submission 経路で `AUTH PLAIN` / `AUTH LOGIN`、`AUTH LOGIN` initial response、認証失敗後の再試行を実装。詳細は [rfc_4954_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_4954_gap.md) |
| RFC 6409 | Message Submission | 一部対応 | Submission リスナ、認証必須化、送信者ドメイン制約、`STARTTLS` 後の再認証要求を実装。詳細は [rfc_6409_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_6409_gap.md) |
| RFC 6531 | SMTPUTF8 | 非対応（方針確定） | `SMTPUTF8` パラメータと UTF-8 メールアドレスは明示的に拒否（`555`/`553`） |
| RFC 7208 | SPF | 一部対応 | `ip4`, `ip6`, `a`, `mx`, `include`, `exists`, `ptr`, `redirect`, `exp`, 主要 macro 展開、lookup 制限、複数 `v=spf1` の `permerror`、HELO/MAIL FROM ポリシー分離を実装。高度な macro transformer は未対応。詳細は [rfc_7208_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_7208_gap.md) |
| RFC 6376 | DKIM | 一部対応 | RSA の受信時 DKIM 検証、複数署名評価、`h/bh/b/l/t/x`、canonicalization、送信時 DKIM 署名を実装。Ed25519 は未対応。詳細は [rfc_6376_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_6376_gap.md) |
| RFC 7489 | DMARC | 一部対応 | SPF/DKIM alignment、`p/sp/pct/fo/rf/ri/rua/ruf`、サブドメインポリシー、`fo`/`rf` の RFC 準拠パース、集計/失敗レポート生成を実装。詳細は [rfc_7489_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_7489_gap.md) |
| RFC 8617 | ARC | 一部対応 | ARC chain の構造検証・暗号検証、`i=` 連番検証、複数 hop 検証、ARC ヘッダ未付与メールへの署名付与、失敗時ポリシーを実装。既存 chain の継続署名は未対応。詳細は [rfc_8617_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_8617_gap.md) |
| RFC 8461 | MTA-STS | 一部対応 | TXT `id` 検証、policy 取得・キャッシュ、`text/plain` 検証、stale 利用、`mode=enforce/testing`、安全なロールオーバー、MX wildcard 制約を実装。詳細は [rfc_8461_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_8461_gap.md) |
| RFC 7672 | DANE for SMTP | 一部対応 | TLSA取得、CNAME 展開、`DANE-TA(2)` の証明書名チェック、優先適用（DANE > MTA-STS）を実装。詳細は [rfc_7672_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_7672_gap.md) |
| RFC 3464 | DSN | 対応済み（実装範囲内） | DSN パース、DSN 生成（hard/soft bounce）、loop 防止、`Reporting-MTA` / `Status` 検証、相互運用テストを実装。詳細は [rfc_3464_gap.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/rfc_3464_gap.md) |

## 実行方法

```bash
go run ./cmd/kuroshio
```

デフォルトでは `:2525` でSMTP待受します。

## 設定

設定方法、主要環境変数、YAML サンプル、Rate Limit 設定、Kafka Queue モードの例は [configuration.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/configuration.md) にまとめています。

## 補足

このリポジトリのコアは、SMTPプロトコル処理を外部SMTPライブラリに依存せず実装しています。

## 開発方針 (TDD)

今後の機能追加・修正は、以下の順で進めます。

1. 先に失敗するテストを書く (`Red`)
2. 最小実装でテストを通す (`Green`)
3. 振る舞いを維持したまま整理する (`Refactor`)

## SMTP Conformance Test

SMTP RFC の主要要件を `internal/smtp` のコンフォーマンステストで確認できます。

```bash
go test ./internal/smtp -run '^TestSMTPConformance$' -v
```

## DNS結合テスト環境

受信側の `mailauth`（SPF/DMARC）と送信側の DANE/MTA-STS の検証用に、
DNS を含む `docker compose` 環境を用意しています。

```bash
./scripts/integration/run_dns_env_tests.sh
```

詳細は `test/integration/README.md` を参照してください。

## SLO/SLI Monitoring

- `/metrics`: Prometheus metrics
- `/slo`: 現在の SLI/SLO 判定結果（JSON, breach時は HTTP 503）
- DMARC集計メトリクス（受信時）:
  - `smtp_auth_dmarc_result_<result>_total`（例: `fail`, `pass`, `none`）
  - `smtp_auth_dmarc_policy_<policy>_total`（例: `reject`, `quarantine`, `none`）
  - `smtp_auth_action_<action>_total`（例: `accept`, `quarantine`, `reject`）

Prometheus alert rule の雛形は [orinoco_slo_rules.yml](/home/tamago/ghq/github.com/tamago/kuroshio-mta/deploy/monitoring/prometheus/orinoco_slo_rules.yml) に配置しています。

## HA Reference

- リファレンス構成とフェイルオーバー手順:
  [ha_reference.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/architecture/ha_reference.md)
- 障害注入ドリル補助スクリプト:
  `scripts/chaos/run_ha_drill.sh`

## Load / Chaos Testing

- 負荷試験 runbook:
  [load_chaos.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/runbooks/load_chaos.md)
- 単体シナリオ実行:
  `scripts/load/run_smtp_load.sh normal 127.0.0.1:2525`
- カオス併用スイート:
  `scripts/chaos/run_load_chaos_suite.sh 127.0.0.1:2525 --apply ./var/load-chaos/results.ndjson`
- 容量計画表への整形:
  `scripts/load/plan_capacity.sh ./var/load-chaos/results.ndjson`

## Admin API

- runbook:
  [admin_api.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/runbooks/admin_api.md)
- 最小CLI:
  `scripts/admin/orinoco_admin.sh`
- 対応操作:
  `suppression` 一覧/追加/削除、`retry` / `dlq` 一覧、再投入
- 認可:
  Bearer token + role（`viewer` / `operator` / `admin`）

## Reputation Controls

- runbook:
  [reputation_ops.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/runbooks/reputation_ops.md)
- 可視化:
  `GET /reputation`
- 記録:
  `scripts/admin/orinoco_admin.sh record-complaint gmail.com`
  `scripts/admin/orinoco_admin.sh record-tlsrpt gmail.com false`

## DR Backup / Restore

- DR（Disaster Recovery, 災害対策）向け runbook:
  [dr_backup_restore.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/runbooks/dr_backup_restore.md)
- バックアップ:
  `scripts/dr/backup_queue.sh ./var/queue ./var/backups`
- リストア:
  `scripts/dr/restore_queue.sh ./var/backups/orinoco-queue-<timestamp>.tar.gz ./var/queue --force`
- DRドリル:
  `scripts/chaos/run_dr_drill.sh ./var/queue ./var/backups --apply`

## Compliance Basics

- ログは `slog(JSON)` で出力
- メールアドレス等のPIIはログ出力時にマスキング
- `sent / mail.dlq / mail.dlq/poison` は保持期間ポリシーに基づき自動削除

## Security

- 脆弱性スキャン: `.github/workflows/security.yml` で `govulncheck` を実行
- SBOM生成: `scripts/security/generate_sbom.sh`
- 詳細方針: [secrets_and_supply_chain.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/security/secrets_and_supply_chain.md)
