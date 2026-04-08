# メール認証を試す

`kuroshio-mta` が持つ SPF / DKIM / DMARC / ARC 周辺の主要機能を、どこから確認するとよいかをまとめたハンズオンです。
このページでは CoreDNS と policy server を `docker compose` で起動して、
SPF / DMARC の評価と送信署名まわりを確認する流れに寄せています。

## 何を確認するか

- 受信時の SPF / DKIM / DMARC / ARC 評価
- 送信時の DKIM / ARC 署名付与
- DMARC レポートや評価の前提となる実装範囲

## 前提

- [Getting Started](/getting-started) を完了している
- Docker と `docker compose` が使える

## 1. tutorial 用の DNS / Web 環境を起動する

この tutorial 用に、CoreDNS と MTA-STS policy 配信用 Web サーバを
`examples/tutorials/dns-services/` に用意しています。

```bash
docker compose -f examples/tutorials/dns-services/compose.yaml up -d
```

DNS レコードは zone file として管理しています。

- [example.test.db](https://github.com/tamago0224/kuroshio-mta/blob/main/examples/tutorials/dns-services/example.test.db)
- [outbound.test.db](https://github.com/tamago0224/kuroshio-mta/blob/main/examples/tutorials/dns-services/outbound.test.db)

まずは DNS と Web サーバが実際に立っていることを確認します。

```bash
docker compose -f examples/tutorials/dns-services/compose.yaml exec dns-client \
  nslookup -type=txt example.test 172.29.0.53

docker compose -f examples/tutorials/dns-services/compose.yaml exec dns-client \
  nslookup -type=txt _dmarc.example.test 172.29.0.53

docker compose -f examples/tutorials/dns-services/compose.yaml exec dns-client \
  wget -qO- http://172.29.0.80/.well-known/mta-sts.txt
```

## 2. SPF / DMARC の integration test を実行する

```bash
docker compose -f examples/tutorials/dns-services/compose.yaml run --rm tester \
  go test -tags=integration ./integration -run TestMailAuthSPFAndDMARCWithDNSMock -v
```

`spf result=pass` と `dmarc result=pass` の系統で成功すれば、
CoreDNS を使った SPF / DMARC 評価が通っています。

## 3. まず実装範囲を押さえる

細かな対応状況は RFC ギャップメモにまとまっています。

- SPF: [RFC 7208 Gap Note](/rfc_7208_gap)
- DKIM: [RFC 6376 Gap Note](/rfc_6376_gap)
- DMARC: [RFC 7489 Gap Note](/rfc_7489_gap)
- ARC: [RFC 8617 Gap Note](/rfc_8617_gap)

## 4. 正規化と判定前提を確認する

認証評価の前段にある envelope や生成メッセージの扱いは、[Normalization Policy](/architecture/normalization_policy) に整理されています。

特に見るとよい項目:

- envelope sender / recipient の扱い
- `postmaster` の補完
- DSN / DMARC report 生成時の正規化

## 5. 送信署名を試す

DKIM / ARC の送信署名付与は unit test から追うのが最短です。

```bash
go test ./internal/dkim -run TestFileSignerSignInjectsDKIMHeader -v
go test ./internal/dkim -run TestARCFileSignerSignInjectsARCSet -v
```

`DKIM-Signature:` や `ARC-Seal:` が先頭に付くことを確認できます。

## 6. 受信評価の関連テストを広めに回す

SPF / DKIM / DMARC の受信評価は `mailauth` パッケージのテストから追えます。

```bash
go test ./internal/mailauth ./internal/smtp
```

## 7. 後片付け

```bash
docker compose -f examples/tutorials/dns-services/compose.yaml down -v
```

## 8. 次に読むドキュメント

- TLS 配送ポリシーも試す: [TLS 配送ポリシーを試す](/tutorials/tls-policy)
- DSN を試す: [RFC 3464 Gap Note](/rfc_3464_gap)
- Reputation 系の運用を見る: [Reputation Ops](/runbooks/reputation_ops)
