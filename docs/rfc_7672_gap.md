# RFC 7672 Gap Note

`kuroshio-mta` の DANE 実装は、送信時の TLSA 取得と証明書照合を対象にしています。

## 現在カバーしている内容

- MX ごとの `_25._tcp.<mx-host>` TLSA 取得
- DNS 応答の AD bit を使った DNSSEC 保護前提の usable 判定
- `2 0 1`, `2 1 1`, `3 0 1`, `3 1 1` プロファイルの照合
- CNAME で参照される TLSA owner name の探索
- DANE 優先、MTA-STS 次順位の配送ポリシー適用
- `DANE-TA(2)` のときの証明書名チェック

## 実装範囲外として扱う内容

- SMTP hop 名以外の追加 reference identifier まで含む完全な名前照合戦略
- TLSA fetch の TCP fallback や DNS メッセージサイズ最適化
- DNSSEC 検証自体のローカル実装
- 証明書更新と TLSA 更新の運用オーケストレーション

## 判断メモ

README の RFC 7672 行は、上記の送信側 TLSA 検証範囲を指します。
RFC 7672 のすべての相互運用シナリオを網羅する全面実装を意味するものではありません。
