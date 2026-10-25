# DKIM2 受信検証実験

## 位置付け

この文書は DKIM2 の受信検証 PoC と段階導入の判断基準を記録します。DKIM2 は作業中の Internet-Draft であり、この実装を標準準拠とは表示しません。既存の DKIM1、ARC、DMARC の検証・受理判断は維持します。

2026-10-25 時点で、実装が固定する文書は次のとおりです。

| 文書 | 固定 revision | 用途 |
| --- | --- | --- |
| DKIM2 signature specification | `draft-ietf-dkim-dkim2-spec-07` | Message-Instance、署名 chain、recipe、envelope 結合 |
| DKIM2 DNS specification | `draft-ietf-dkim-dkim2-dns-02` | 鍵探索と鍵 record |
| DKIM2 operational BCP | `draft-ietf-dkim-dkim2-bcp-01` | 運用・移行上の考慮事項 |

Issue 作成時の `spec-06` / `dns-00` から更新されています。実装着手時に IETF Datatracker を再確認し、コード中の `SpecificationRevision`、`DNSRevision`、`BCPRevision` とこの表を同時に更新します。revision 更新は互換性変更として扱い、既存ベクトルの再評価なしに追従しません。

## 現在の実装範囲

`internal/dkim2` は、既定の受理判断から独立した観測専用の検証器です。

- Message-Instance と DKIM2-Signature の組、連続した `m=` / `i=`、必須 tag、重複 tag を検査する
- Message-Instance の hash tag、signature algorithm と Base64 表現を構文検査する
- 最新 signature の `mf=` / `rt=` と SMTP envelope を照合する
- recipe の Base64、JSON、サイズ、深さを検査する
- message、chain、recipe、将来の DNS 照会数・timeout に明示的な上限を持つ
- recipe がない現在の message について SHA-256 / SHA-512 の header・body hash を検証する
- 制限付き resolver で DNS key record を取得し、RSA-SHA256 / Ed25519-SHA256 署名を検証する

`Evaluate` の構造検査を通過しても結果は `neutral` です。`Verify` は recipe を逆適用した各 instance の hash、DNS 鍵、対応する暗号署名をすべて検証した場合だけ `pass` を返します。body が再構成不能と明示された recipe は検証不能です。

SMTP 受信では DKIM1 / ARC / DMARC の評価後に `mailauth.DKIM2` へ独立して格納し、`smtp_auth_dkim2_result_<result>` metric と `smtp.auth.dkim2.result` trace attribute で観測します。DKIM2 の結果は `Action`、Authentication-Results header、DMARC alignment、SMTP 応答、queue 保存形式へ入力しません。

`mldsa44-ed25519-sha512` は Go 標準ライブラリに公開 API がないため未実装です。対象 draft は verifier にこの algorithm を要求しているため、この制約が残る実験を標準準拠とは扱いません。

既定上限は次のとおりです。

| 資源 | 上限 |
| --- | ---: |
| message | 10 MiB |
| chain | 16 instances / signatures |
| decoded recipe | 64 KiB |
| recipe JSON depth | 16 |
| DNS queries | 8 |
| DNS TXT response | 64 KiB |
| DNS timeout | 2秒 |

入力サイズ、chain 深さ、recipe サイズ・深さの超過は `permerror` として観測します。DNS timeout と照会上限の超過は `temperror` として観測します。

## 段階導入

### Phase 1: bounded parser

完了済みです。外部 I/O を行わず、構造、chain、envelope、recipe と資源上限をテストベクトルで固定します。

### Phase 2: cryptographic shadow verification（PoC 完了）

- Message-Instance の header/body hash と recipe 再構成を検証する
- `selector._domainkey.domain` の鍵を DNS から取得し、照会数・応答サイズ・timeout を制限する
- RSA、Ed25519 を検証する（複合 algorithm は未実装）
- SMTP の受理判断から分離した result、metric、trace attribute で観測する
- 有効署名、改変署名、期限切れ、欠落・重複 chain、不正鍵、不正 recipe、DNS timeout のベクトルを用意する
- DKIM2 結果を独立した metric/log として記録し、DKIM1/ARC/DMARC の結果構造や受理判断へ入力しない

### Phase 3: forwarding and signing experiment

- 送信、内容を変更しない中継、recipe を伴う変更を別機能として実装する
- 複数宛先を分割するときの `rt=`、Bcc の受信者秘匿、nonce の再利用防止を検証する
- queue の再送で timestamp、nonce、envelope が変わる境界を定義し、保存形式の互換性を確保する
- null reverse-path の DSN と通知ループ防止を確認する
- DKIM1 / ARC との併用を維持し、DKIM2 の結果を DKIM1 alignment として DMARC に流用しない

## 採用条件

本番の受理判断または送信を有効にするには、少なくとも次を満たす必要があります。

1. 対象 draft revision と実装差分がレビューされ、保存済み message・queue・管理 API の互換性が定義されている
2. recipe 再構成、chain、DNS、暗号処理の資源上限と失敗時動作がテストされている
3. 複数実装との相互運用結果に、相手実装と双方の対応 revision が記録されている
4. Bcc、複数宛先、転送、再送、DSN の情報漏えいと replay リスクが解消されている
5. shadow 観測で既存 DKIM1 / ARC / DMARC と受理率に意図しない変化がない
6. Internet-Draft から RFC への変更、または採用する revision の安定性を再評価している

Stalwart の公開告知は `draft-04` 時点の情報です。`spec-07` / `dns-02` との相互運用を確認した事実としては扱いません。確認を実施した場合だけ、使用した version、revision、成功・失敗ケースをこの文書へ追記します。

## 参照

- [DKIM2 signature specification -07](https://datatracker.ietf.org/doc/html/draft-ietf-dkim-dkim2-spec-07)
- [DKIM2 DNS specification -02](https://datatracker.ietf.org/doc/html/draft-ietf-dkim-dkim2-dns-02)
- [DKIM Working Group documents](https://datatracker.ietf.org/wg/dkim/documents/)
- [Normalization Policy](./normalization_policy.md)
- [RFC 6376 gap note](../rfc_6376_gap.md)
