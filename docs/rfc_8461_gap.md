# RFC 8461 Gap Note

`kuroshio-mta` の MTA-STS 実装は、送信時のポリシー取得と適用を対象にしています。

## 現在カバーしている内容

- `_mta-sts.<domain>` TXT からの policy `id` 検出
- HTTPS による policy 取得、証明書検証、redirect 拒否
- `text/plain` media type の確認
- `mode=enforce/testing/none`、`mx`、`max_age` の policy パース
- stale policy 利用、TXT `id` 変化時の安全な rollover
- DANE 優先、MTA-STS 次順位の配送ポリシー適用
- wildcard MX の左端 1 ラベル一致制約

## 実装範囲外として扱う内容

- smart host 固有の policy domain 選択ロジック
- policy fetch の proactive refresh スケジューリング
- TLSRPT の収集と分析
- 応答サイズ上限や fetch cadence の細かな運用最適化

## 判断メモ

README の RFC 8461 行は、上記の送信側 enforcement 実装を指します。
RFC 8461 の運用上の推奨事項や周辺 RFC を含む全面実装を意味するものではありません。
