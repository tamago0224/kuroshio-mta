# RFC 3464 Gap Note

`kuroshio-mta` の DSN 実装は、配送失敗通知と bounce 処理に必要な範囲を対象にしています。

## 現在カバーしている内容

- `message/delivery-status` の per-message / per-recipient block パース
- `Reporting-MTA` 必須、`Status` 書式、`Action` と enhanced status class の整合検証
- 複数 recipient block、folded header、`Will-Retry-Until` などの代表的な相互運用ケース
- hard bounce / soft bounce 向け DSN 生成
- `Auto-Submitted` と null reverse-path による通知ループ防止
- 生成した DSN を再度パースできる相互運用テスト

## 実装範囲外として扱う内容

- RFC 3461 の envelope-level DSN 拡張を使った通知要求制御
- 外部 MTA から受け取った任意の extension field の意味解釈や保存
- 生成 DSN への original message / original headers 添付ポリシーの細かな選択肢
- 配送成功通知や relay / expand 通知の自動生成

## 判断メモ

README の `対応済み（実装範囲内）` は、上記の bounce 通知ユースケースに限定した表現です。
RFC 3464 の全オプションや関連 RFC まで含む完全実装を意味するものではありません。
