# RFC 3207 Gap Note

`kuroshio-mta` の STARTTLS 実装は、受信側 SMTP サーバと送信側 SMTP クライアントでの TLS 昇格フローを対象にしています。

## 現在カバーしている内容

- 受信側 EHLO での `STARTTLS` 広告
- `STARTTLS` 成功後の TLS ハンドシェイク
- TLS 昇格後の SMTP セッション再初期化と EHLO 再実行要求
- TLS 有効時に `STARTTLS` を再広告しない挙動
- 送信側での `STARTTLS` 検出、TLS 昇格、再 EHLO
- TLS 必須ポリシー時の失敗処理

## 実装範囲外として扱う内容

- クライアント証明書認証
- 明示的な downgrade attack 検知ロジック
- TLS-RPT など周辺レポート仕様そのもの

## 判断メモ

README の RFC 3207 行は、SMTP セッション中の STARTTLS 昇格と再初期化の実装範囲を指します。
この範囲では受信側・送信側の主要挙動をカバーできているため、`対応済み（実装範囲内）` を維持します。
