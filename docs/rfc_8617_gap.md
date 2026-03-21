# RFC 8617 Gap Note

`orinoco-mta` の ARC 実装は、受信時の chain 検証と ARC ヘッダ未付与メールへの署名付与を対象にしています。

## 現在カバーしている内容

- `ARC-Seal` / `ARC-Message-Signature` / `ARC-Authentication-Results` の 3 点セット検証
- `i=` の連番、重複、欠落、`cv` 制約の検証
- 1 hop と複数 hop の ARC chain に対する暗号検証テスト
- 受信ポリシーとしての `accept` / `quarantine` / `reject`
- ARC ヘッダがまだ無いメッセージへの新規 ARC セット付与
- 鍵ローテーション時の ARC 署名キー再読込

## 実装範囲外として扱う内容

- 既存 ARC chain を持つメッセージに対する次 hop の継続署名
- `cv=fail` を伴う受信チェーンを引き継いだうえでの再署名方針
- 外部 MTA 実装との差異を吸収するための広範な相互運用マトリクス

## 判断メモ

README の RFC 8617 行は、受信検証と単独メッセージへの ARC 署名付与を指します。
既存 ARC chain の継続署名が未実装のため、現時点では `対応済み` ではなく `一部対応` のままとします。
