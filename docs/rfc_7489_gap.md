# DMARC implementation and RFC 9989 migration note

`dmarc_policy_mode` で RFC 7489互換評価とRFC 9989評価を段階的に切り替えます。既定の `shadow` は旧評価を本判定に使い、RFC 9989結果を `DMARCResult.DMARCbis` に付けて比較します。`enforce` はRFC 9989結果を本判定、Authentication-Results、レポートに使います。`legacy` は明示的なrollback用です。

RFC 9989 は RFC 7489 を置き換え、固定 PSL suffix による組織ドメイン決定を DNS Tree Walk に置き換えています。[RFC 9989](https://www.rfc-editor.org/rfc/rfc9989.html)

## RFC 9989 shadow 評価

- `EvalDMARCbis` は Author Domain の完全一致 record を先に調べ、続けて DNS Tree Walk を実施します。Tree Walk は最大8照会に制限します。
- 単一の有効 record のみ採用し、`p`, `sp`, `np`, `psd`, `t`, `aspf`, `adkim` を構文検証します。複数の DMARC record は採用しません。
- SPF / DKIM relaxed alignment は DNS Tree Walk で選んだ組織ドメインを比較し、strict は完全一致します。`psd=y/n`、存在する/しない Author Domain の `sp/np`、`t=y` による一段階のpolicy緩和を結果へ反映します。`t` の既定値は `n` です。
- 一時 DNS 障害は `temperror` として shadow 結果に記録し、配送判断には使いません。
- `shadow` は旧判定を実行した後にRFC 9989結果を追加し、受理actionを変更しません。
- `enforce` では `pct`, `rf`, `ri` を判定に使いません。RFC 9989の `np`, `psd`, `t` とTree Walk結果を使い、DNS一時障害から旧方式へ自動fallbackしません。

移行前に production 相当のトラフィックで `smtp_auth_dmarc_engine_mismatch_total`、`smtp_auth_dmarcbis_dns_error_total`、DNS query数・遅延、`psd/np/t` とstrict alignmentの差を観測してください。差分とDNSエラーが許容範囲に収まったら `dmarc_policy_mode: enforce` に変更して再起動します。問題時は `shadow`、緊急時だけ `legacy` に戻して再起動します。設定読込時のaudit logに選択modeが残ります。

`enforce` で `temperror` になった場合はlocal policyとして受理し、旧方式のpass/rejectを採用しません。SPFの明示的enforceとARC failure policyはDMARCエンジンとは別のlocal policyとして引き続き優先されます。RFC 9989の本判定結果は `DMARCResult.Result/Policy`、比較用の旧結果は `LegacyResult/LegacyPolicy` に保持します。

各送信組織は移行前にAuthor Domainと必要なOrganizational Domainへ明示的なDMARC recordを公開してください。strict alignment (`adkim=s` / `aspf=s`) を使う場合は、実際のDKIM `d=` またはSPF domainがAuthor Domainと完全一致することを確認します。PSD境界に依存する構成では `psd` と `np` の結果をshadowメトリクスで確認してから切り替えます。

## RFC 7489 互換評価

現在の本判定は、受信時評価と送信時レポート生成の基本フローを対象にしています。

## 現在カバーしている内容

- SPF / DKIM alignment 判定
- `p`, `sp`, `pct`, `aspf`, `adkim`, `fo`, `rf`, `ri`, `rua`, `ruf` の主要タグ解釈
- 既存固定 suffix 表による組織ドメインベースのサブドメインポリシー継承
- `pct` サンプリングとポリシー適用
- RFC 9990 aggregate report の生成（運用詳細は [DMARC aggregate reports](./runbooks/dmarc_aggregate_reports.md)）

## 実装範囲外として扱う内容

- RFC 9991 の RUF failure report 送信。PIIや本文の外部開示リスクがあるためサポートせず、`ruf` を公開したドメインに対しても生成しません
- XML aggregate report の受信 / 解析パイプライン
- すべての異常 DMARC レコードに対する厳密なエラー分類

## 判断メモ

RFC 7489互換データのDB migrationは不要です。キュー済みメッセージは評価結果を保存しておらず、切替後に再評価しません。日次aggregate DBは探索方式をtupleに含むため、切替期間をまたいでも異なる評価を混在させません。
