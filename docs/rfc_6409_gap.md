# RFC 6409 Gap Note

`kuroshio-mta` の Message Submission 実装は、認証必須の Submission listener を対象にしています。

## 現在カバーしている内容

- Submission listener の分離
- 認証必須化と未認証 `MAIL FROM` の拒否
- 認証済みユーザと送信者ドメインの整合制約
- Submission での `AUTH` 広告
- `STARTTLS` 後のセッションリセットと再認証要求

## 実装範囲外として扱う内容

- 追加の Submission policy（本文書換え、MUA 向け拡張ヘッダ付与など）
- `BURL` や将来的な関連拡張
- MSA 固有の詳細な運用ポリシー管理 UI

## 判断メモ

README の RFC 6409 行は、上記の認証前提 Submission フローを指します。
RFC 6409 の周辺拡張やすべての運用パターンを含む全面実装を意味するものではありません。
