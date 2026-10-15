# RFC 8617 Gap Note

`kuroshio-mta` の ARC 実装は、受信時の chain 検証、新規署名、既存 chain への継続署名を対象にしています。

## 現在カバーしている内容

- `ARC-Seal` / `ARC-Message-Signature` / `ARC-Authentication-Results` の 3 点セット検証
- `i=` の連番、重複、欠落、`cv` 制約の検証
- 1 hop と複数 hop の ARC chain に対する暗号検証テスト
- 受信ポリシーとしての `accept` / `quarantine` / `reject`
- ARC ヘッダがまだ無いメッセージへの新規 ARC セット付与
- 既存チェーンを暗号検証して次の `i=n+1; cv=pass` セットを追加
- 最新 AMS と全 ARC-Seal を検証（過去の AMS が本文変更で無効になっても、それだけでは chain を失敗にしない）
- AMS の署名対象から Authentication-Results と ARC ヘッダを除外し、既存 DKIM-Signature を含める
- 鍵ローテーション時の ARC 署名キー再読込

## 継続署名と失敗時の方針

[RFC 8617 Sections 5.1–5.2](https://www.rfc-editor.org/rfc/rfc8617.html#section-5.1) に従い、配送時に DKIM 署名を付けた後で ARC を検証・署名します。

- 検証結果はメッセージの Authentication-Results の `arc=pass` を信用せず、暗号検証から取得します
- 正常チェーンへの ARC-Seal は、全セットを instance 昇順に AAR / AMS / AS の順で署名します
- 欠落・重複・署名不正などの失敗は、最大 instance に 1 を加えた `cv=fail` セットで記録します。この seal の署名範囲は追加するセットだけです
- 最新セットがすでに `cv=fail` の場合は、それ以上 ARC セットを追加しません。失敗した chain を `pass` に戻しません
- instance が不正で次の値を安全に決定できない場合、または既存 instance が 50 以上の場合は、既存 ARC ヘッダを保持して追加署名を省略します
- 受信検証は 50 セットを上限とし、`cv=fail` を含む chain を失敗と判定します
- 元のヘッダ・本文は保持します。AAR にはローカル authserv-id の結果を使用し、ARC の検証結果を今回の値で記録します

`ARCFileSigner.SignWithChainValidation` に渡す検証結果は、呼び出し側が暗号検証した値である必要があります。配送処理はこの検証を行います。結果を渡さない `Sign` は、既存チェーンを保守的に失敗として扱います。

## 実装範囲外として扱う内容

- 外部 MTA 実装との差異を吸収するための広範な相互運用マトリクス

## 判断メモ

受信検証・新規署名・継続署名を対象とし、生成した複数 hop の chain を受信検証へ戻すテストを行っています。
外部 MTA との相互運用を全面的に保証するものではありません。
