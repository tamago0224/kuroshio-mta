# RFC 7208 Gap Note

`kuroshio-mta` の SPF 実装は、受信時の送信元検証と配送ポリシー判断に必要な範囲を対象にしています。

## 現在カバーしている内容

- `ip4`, `ip6`, `a`, `mx`, `include`, `exists`, `ptr`, `all` の主要メカニズム
- `redirect` と `exp` modifier
- 主要 SPF macro 展開
- DNS lookup 上限と void lookup 制限
- include / redirect 循環検出と HELO / MAIL FROM ポリシー分離
- 複数 `v=spf1` レコードを `permerror` とする境界条件

## 実装範囲外として扱う内容

- macro の transformer や delimiter を含む完全実装
- 実運用 DNS の曖昧応答を含む広範な相互運用マトリクス
- SMTP reply text への explanation 反映方法の細かな運用差異

## 判断メモ

README の RFC 7208 行は、受信判定に必要な主要メカニズムとポリシー制御を指します。
macro の一部高度機能や運用差分は未実装のため、現時点では `対応済み` ではなく `一部対応` のままとします。
