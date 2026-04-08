# TLS 配送ポリシーを試す

`kuroshio-mta` の STARTTLS、MTA-STS、DANE といった配送時セキュリティ機能を確認するための入口です。
このページでは、手元の test と CoreDNS ベースの tutorial 環境を組み合わせて、
受信側 STARTTLS と送信側 MTA-STS / DANE を実際に確認する流れにしています。

## 何を確認するか

- SMTP セッションでの STARTTLS 昇格
- 送信側の MTA-STS / DANE の優先と失敗時挙動
- TLS 周辺の実装範囲

## 前提

- [Getting Started](/getting-started) を完了している
- Docker と `docker compose` が使える

## 1. STARTTLS の実装範囲を確認する

まずは [RFC 3207 Gap Note](/rfc_3207_gap) を読み、`kuroshio-mta` がどこまで STARTTLS を実装しているかを確認します。

確認ポイント:

- EHLO で `STARTTLS` を広告するか
- TLS 昇格後に再 EHLO を要求するか
- すでに TLS 下のセッションで再度 `STARTTLS` を拒否するか

## 2. STARTTLS 関連テストを実行する

受信側の STARTTLS 昇格は、次のテストで確認できます。

```bash
go test ./internal/smtp -run 'TestSTARTTLSWithoutTLSConfigReturns454|TestSTARTTLSWithTLSConfigUpgradesConnection' -v
```

必要なら SMTP 基本フロー全体としては次も使えます。

```bash
go test ./internal/smtp -run '^TestSMTPConformance$' -v
```

## 3. tutorial 用の DNS / Web 環境を起動する

送信側の MTA-STS / DANE は、CoreDNS と policy server を含む tutorial 用 compose で確認できます。

```bash
docker compose -f examples/tutorials/dns-services/compose.yaml up -d
```

まずは DNS と policy を直接確認します。

```bash
docker compose -f examples/tutorials/dns-services/compose.yaml exec dns-client \
  nslookup -type=mx outbound.test 172.29.0.53

docker compose -f examples/tutorials/dns-services/compose.yaml exec dns-client \
  nslookup -type=txt _mta-sts.outbound.test 172.29.0.53

docker compose -f examples/tutorials/dns-services/compose.yaml exec dns-client \
  wget -qO- http://172.29.0.80/.well-known/mta-sts.txt
```

## 4. 送信側ポリシーを追う

MTA-STS と DANE の実装範囲は次を参照します。

- [RFC 8461 Gap Note](/rfc_8461_gap)
- [RFC 7672 Gap Note](/rfc_7672_gap)

README では DANE が MTA-STS より優先される実装方針も案内しています。

integration test としては次を実行できます。

```bash
docker compose -f examples/tutorials/dns-services/compose.yaml run --rm tester \
  go test -tags=integration ./integration -run 'TestOutboundDANELookupWithDNSMock|TestOutboundPolicyPrecedenceDANEOverMTASTS|TestMTASTSPolicyFetchFromPolicyService' -v
```

これで次を確認できます。

- `dnsmock` から TLSA を引けること
- DANE が有効なときに TLS 必須で配送すること
- `policy` サービスから MTA-STS policy を取得できること

## 5. 後片付け

```bash
docker compose -f examples/tutorials/dns-services/compose.yaml down -v
```

## 6. 運用確認につなげる

配送まわりの健全性を見る場合は、SLO runbook もあわせて使います。

- [SLO Delivery](/runbooks/slo_delivery)
- [SLO Retry](/runbooks/slo_retry)

## 次に読むページ

- 認証系を見る: [メール認証を試す](/tutorials/mail-auth)
- 負荷下で試す: [Load / Chaos](/runbooks/load_chaos)
