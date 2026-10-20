# DMARC implementation and RFC 9989 migration note

現行 `Evaluate` は RFC 7489 互換評価を本判定に使い、`EvaluateWithDMARCbis` は RFC 9989 結果を `DMARCResult.DMARCbis` に付けて比較します。shadow 結果は配送 action を変更しません。呼び出し側が shadow 関数を使うよう切り替えるまでは通常処理へ追加 DNS 照会を行いません。

RFC 9989 は RFC 7489 を置き換え、固定 PSL suffix による組織ドメイン決定を DNS Tree Walk に置き換えています。[RFC 9989](https://www.rfc-editor.org/rfc/rfc9989.html)

## RFC 9989 shadow 評価

- `EvalDMARCbis` は Author Domain の完全一致 record を先に調べ、続けて DNS Tree Walk を実施します。Tree Walk は最大8照会に制限します。
- 単一の有効 record のみ採用し、`p`, `sp`, `np`, `psd`, `t`, `aspf`, `adkim` を構文検証します。複数の DMARC record は採用しません。
- SPF / DKIM relaxed alignment は DNS Tree Walk で選んだ組織ドメインを比較し、strict は完全一致します。`psd=y/n`、存在する/しない Author Domain の `sp/np`、`t=n` を結果へ反映します。
- 一時 DNS 障害は `temperror` として shadow 結果に記録し、配送判断には使いません。
- `EvaluateWithDMARCbis` は旧判定を実行した後に shadow を追加します。shadow の有効化は自動ではありません。

移行前に production 相当のトラフィックで旧新結果、DNS query 数・遅延、`psd/np/t` と strict alignment の差を観測してください。shadow は比較用 API で、保存・メトリクス・本判定切替の運用スイッチはまだ提供しません。切替後は失敗や DNS エラーを理由に旧方式の pass へ fallback しない設計が必要です。

## RFC 7489 互換評価

現在の本判定は、受信時評価と送信時レポート生成の基本フローを対象にしています。

## 現在カバーしている内容

- SPF / DKIM alignment 判定
- `p`, `sp`, `pct`, `aspf`, `adkim`, `fo`, `rf`, `ri`, `rua`, `ruf` の主要タグ解釈
- 既存固定 suffix 表による組織ドメインベースのサブドメインポリシー継承
- `pct` サンプリングとポリシー適用
- 集計 / 失敗レポート生成

## 実装範囲外として扱う内容

- 外部宛先レポート配送の完全な認可検証
- XML aggregate report の受信 / 解析パイプライン
- RFC 9990/9991 の集約・failure report 実装（#328/#329 を参照）
- すべての異常 DMARC レコードに対する厳密なエラー分類

## 判断メモ

RFC 7489 の運用面や周辺仕様を含む全面実装を意味するものではありません。RFC 9989 の完全移行には shadow 結果の永続的な観測、設定による段階切替、旧形式保存データとの互換確認が残っています。
