# RFC 3461 / RFC 3464 Gap Note

`kuroshio-mta` の DSN 実装は、RFC 3461 の SMTP 拡張と RFC 3464 の配送状態通知を対象にしています。

## 現在カバーしている内容

- `message/delivery-status` の per-message / per-recipient block パース
- `Reporting-MTA` 必須、`Status` 書式、`Action` と enhanced status class の整合検証
- 複数 recipient block、folded header、`Will-Retry-Until` などの代表的な相互運用ケース
- hard bounce / soft bounce 向け DSN 生成
- EHLO での `DSN` 広告
- `RET` / `ENVID` / `NOTIFY` / `ORCPT` の検証・キュー永続化
- 宛先別の成功・失敗・遅延・通知不要の選択と `RET=FULL` / `RET=HDRS` の反映
- DSN を広告する next hop への envelope パラメータ引継ぎ
- `Auto-Submitted` と null reverse-path による通知ループ防止
- 生成した DSN を再度パースできる相互運用テスト

## 実装範囲外として扱う内容

- 外部 MTA から受け取った任意の extension field の意味解釈や保存
- DSN を広告しない next hop に通知要求を end-to-end で保証すること（通常配送を継続し、パラメータは送信しません）
- 複数宛先を単一 DSN に集約する最適化と relay / expand 通知

## 判断メモ

README の `対応済み（実装範囲内）` は、上記の受信・引継ぎ・通知ユースケースに限定した表現です。
RFC 3464 の全オプションや関連 RFC まで含む完全実装を意味するものではありません。
