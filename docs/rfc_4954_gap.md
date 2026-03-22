# RFC 4954 Gap Note

`kuroshio-mta` の SMTP AUTH 実装は、Submission 経路での基本的なユーザ認証を対象にしています。

## 現在カバーしている内容

- `AUTH PLAIN` と `AUTH LOGIN`
- `AUTH LOGIN` の challenge-response 形式と initial response 形式
- 認証必須 Submission での未認証 `MAIL FROM` 拒否
- 認証失敗後の再試行
- 認証済みユーザと `MAIL FROM` ドメインの整合制約

## 実装範囲外として扱う内容

- CRAM-MD5, SCRAM, OAuth Bearer など追加 SASL mechanism
- `MAIL FROM AUTH=` パラメータ
- SASL security layer negotiation
- 外部 identity provider や多要素認証との統合

## 判断メモ

README の RFC 4954 行は、上記の Submission 向け基本認証フローを指します。
RFC 4954 のすべての SASL 拡張や運用パターンを網羅する全面実装を意味するものではありません。
