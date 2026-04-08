# TLS 配送ポリシーを試す

`kuroshio-mta` の STARTTLS、MTA-STS、DANE といった配送時セキュリティ機能を確認するための入口です。

## 何を確認するか

- SMTP セッションでの STARTTLS 昇格
- 送信側の MTA-STS / DANE の優先と失敗時挙動
- TLS 周辺の実装範囲

## 1. STARTTLS の実装範囲を確認する

まずは [RFC 3207 Gap Note](/rfc_3207_gap) を読み、`kuroshio-mta` がどこまで STARTTLS を実装しているかを確認します。

確認ポイント:

- EHLO で `STARTTLS` を広告するか
- TLS 昇格後に再 EHLO を要求するか
- すでに TLS 下のセッションで再度 `STARTTLS` を拒否するか

## 2. コンフォーマンステストを実行する

STARTTLS を含む SMTP 基本フローは、次のテストで確認できます。

```bash
go test ./internal/smtp -run '^TestSMTPConformance$' -v
```

## 3. 送信側ポリシーを追う

MTA-STS と DANE の実装範囲は次を参照します。

- [RFC 8461 Gap Note](/rfc_8461_gap)
- [RFC 7672 Gap Note](/rfc_7672_gap)

README では DANE が MTA-STS より優先される実装方針も案内しています。

## 4. 運用確認につなげる

配送まわりの健全性を見る場合は、SLO runbook もあわせて使います。

- [SLO Delivery](/runbooks/slo_delivery)
- [SLO Retry](/runbooks/slo_retry)

## 次に読むページ

- 認証系を見る: [メール認証を試す](/tutorials/mail-auth)
- 負荷下で試す: [Load / Chaos](/runbooks/load_chaos)

