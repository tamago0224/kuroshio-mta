# RFC 6152 Gap Note

`orinoco-mta` の 8BITMIME 実装は、受信側 SMTP セッションでの広告と `MAIL FROM BODY=` に応じた本文受理判定を対象にしています。

## 現在カバーしている内容

- EHLO での `8BITMIME` 広告
- `MAIL FROM BODY=8BITMIME` / `BODY=7BIT` の解釈
- `BODY=8BITMIME` 指定時の 8bit 本文受信
- `BODY=7BIT` または未指定時の 8bit 本文拒否
- `SMTPUTF8` 非対応方針との切り分け

## 実装範囲外として扱う内容

- 送信側 SMTP クライアントでの相手 8BITMIME 広告に応じた変換最適化
- 8bit から 7bit への自動 downgrade / MIME 再符号化

## 判断メモ

README の RFC 6152 行は、受信側 8BITMIME 拡張の実装範囲を指します。
この範囲では広告、受理、拒否の主要挙動をテストでカバーできているため、`対応済み（実装範囲内）` を維持します。
