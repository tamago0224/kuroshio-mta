# RFC 7208 Gap Note

`kuroshio-mta` の SPF 実装は、受信時の送信元検証と配送ポリシー判断に必要な範囲を対象にしています。

## 現在カバーしている内容

- `ip4`, `ip6`, `a`, `mx`, `include`, `exists`, `ptr`, `all` の主要メカニズム
- `redirect` と `exp` modifier
- SPF macro の件数指定・反転・delimiter 指定、IPv6 の nibble 展開、uppercase macro の URL エスケープ
- `%{...}` 内の `/`・`=` を CIDR や modifier と区別して処理
- domain-spec と `exp` 本文の macro 制約、不正構文の検出、DNS 名の長さ制限
- DNS lookup 上限と void lookup 制限
- include / redirect 循環検出と HELO / MAIL FROM ポリシー分離
- 複数 `v=spf1` レコードを `permerror` とする境界条件

## Macro 処理

[RFC 7208 Sections 7.1–7.4](https://www.rfc-editor.org/rfc/rfc7208.html#section-7) の transformer と delimiter を扱います。

- `%{d2}` は右側2要素、`%{d2r}` は反転してから右側2要素を選択します
- delimiter は `. - + , / _ =`。指定した文字で分割し、空要素を保持して `.` で再結合します。件数0は不正、大きすぎる件数は全要素を使います
- `%{i}` は IPv4 では dotted quad、IPv6 では32個の nibble、`%{ir}` はその反転です
- uppercase macro は小文字と同じ展開後に RFC 3986 の unreserved 以外を `%HH` にします
- sender / local-part の文字ケースを保持します。受信 SMTP の envelope 正規化については [正規化方針](./architecture/normalization_policy.md) を参照してください
- HELO 評価時の sender は `postmaster@<helo>` を使います
- domain-spec では `c` / `r` / `t` を拒否し、`exp` で取得した本文だけで展開します
- 展開後の DNS 名が253文字を超える場合は左端の label から除去します。空 label・63文字超の label・空白を含む名前は拒否します。末尾の root dot は受理します
- 不正な domain-spec macro は `permerror`。不正・複数レコード・ASCII 制約違反・512文字超の説明文は使用せず、元の `fail` 判定と既定の理由を維持します

## 実装範囲外として扱う内容

- `%{p}` の validated PTR 名選択（現在は `unknown`）と `%{r}` への受信 MTA 名の設定（現在は `unknown`）
- 実運用 DNS の曖昧応答を含む広範な相互運用マトリクス
- SMTP reply text への explanation 反映方法の細かな運用差異

## 判断メモ

主要メカニズムと transformer / delimiter の処理を対象とします。PTR macro の名前選択や広範な DNS 相互運用を含む RFC 全体の完全対応を意味するものではありません。
