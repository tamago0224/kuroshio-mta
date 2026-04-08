# メール認証を試す

`kuroshio-mta` が持つ SPF / DKIM / DMARC / ARC 周辺の主要機能を、どこから確認するとよいかをまとめたハンズオンです。

## 何を確認するか

- 受信時の SPF / DKIM / DMARC / ARC 評価
- 送信時の DKIM / ARC 署名付与
- DMARC レポートや評価の前提となる実装範囲

## 1. まず実装範囲を押さえる

細かな対応状況は RFC ギャップメモにまとまっています。

- SPF: [RFC 7208 Gap Note](/rfc_7208_gap)
- DKIM: [RFC 6376 Gap Note](/rfc_6376_gap)
- DMARC: [RFC 7489 Gap Note](/rfc_7489_gap)
- ARC: [RFC 8617 Gap Note](/rfc_8617_gap)

## 2. 正規化と判定前提を確認する

認証評価の前段にある envelope や生成メッセージの扱いは、[Normalization Policy](/architecture/normalization_policy) に整理されています。

特に見るとよい項目:

- envelope sender / recipient の扱い
- `postmaster` の補完
- DSN / DMARC report 生成時の正規化

## 3. 送信署名を試す

DKIM 署名付与の挙動はテストから追うのが最短です。

```bash
go test ./internal/dkim -run TestFileSignerSignInjectsDKIMHeader -v
```

`DKIM-Signature:` が先頭に付くことを確認できます。

## 4. 受信評価を試す

SPF / DKIM / DMARC の受信評価は `mailauth` パッケージのテストから追えます。

```bash
go test ./internal/mailauth ./internal/smtp
```

SMTP 受信時にどこまで認証結果を扱うかは [README](https://github.com/tamago0224/kuroshio-mta/blob/main/README.md) の RFC 対応表もあわせて確認してください。

## 5. 次に読むドキュメント

- TLS 配送ポリシーも試す: [TLS 配送ポリシーを試す](/tutorials/tls-policy)
- DSN を試す: [RFC 3464 Gap Note](/rfc_3464_gap)
- Reputation 系の運用を見る: [Reputation Ops](/runbooks/reputation_ops)

