# DKIM Signing Profiles and Key Rotation

## 適用範囲

複数送信ドメインの署名を `dkim_profiles_file` の JSON で管理します。
従来の単一 DKIM/ARC 設定と同時には使用できません。従来設定の鍵再読込は維持します。
新しい profile は domain・selector・algorithm・秘密鍵を一組として検証し、
起動時と SIGHUP 時に有効な設定一式を反映します。反映失敗時は旧 profile を維持します。

初期の署名対象は認証済み Submission です。envelope sender と From の両方が
認証 principal の sender scope 内であり、ドメインが同じ場合にだけ署名権限をキューへ保存します。
From は1フィールド・1アドレスを必須とします。`submission_enforce_sender_identity` が無効でも
profile モードのこの検査は行い、違反は DATA の550で拒否します。
static / sqlite の username-domain fallback と、Bearer の明示 scope 必須という既存の権限規則を使います。
外部からの署名用 X ヘッダ、Authentication-Results、任意の domain ヘッダは権限になりません。

未認証の受信や信頼 CIDR からの relay 入力には署名権限を付けず、署名せずに配送します。
管理者による trusted CIDR の指定だけでは、profile 署名を許可しません。
自動生成 DSN / DMARC 通知の null reverse-path は unsigned として扱います。
このモードは SMTP の中継認可や宛先検証を緩めません。

## 設定ファイル

[設定例](../../examples/dkim-profiles.example.json) を参考に、管理者が鍵と DNS を準備してください。
秘密鍵は同梱していません。profile JSON は最大1MiB、同時に最大64ドメインです。

```yaml
# 以下の3項目は削除: dkim_sign_domain / dkim_sign_selector / dkim_private_key_file
# path 自体の変更には再起動が必要
# MTA_DKIM_PROFILES_FILE で上書き可能
dkim_profiles_file: /etc/kuroshio/signing-profiles.json
```

| JSON field | 意味 |
| --- | --- |
| `schema_version` | 必須。`1` |
| `revision` | 必須の運用識別子。非空、128 bytes以内 |
| `unknown_domain` | `reject`（省略時も同じ）または明示的な `unsigned` |
| `profiles` | 空でない profile 配列。domain の重複不可 |
| `domain` | 小文字化した exact signing domain。ワイルドカード不可 |
| `selector` | DNS名として有効な selector。小文字化して使用 |
| `algorithm` | `rsa-sha256` または `ed25519-sha256`。鍵種別との一致必須 |
| `key_file` | 必須。相対パスは JSON ファイルのディレクトリ基準 |
| `headers` | colon 区切り。ASCII 英数字とハイフンの名前のみ。省略時は From/To/Subject/Date/Message-ID。From を必須とする |
| `arc` | 既定 false。有効時は DKIM と同じ profile の鍵で ARC も署名する |

RSA は PKCS#1 / PKCS#8 PEM、Ed25519 は PKCS#8 PEM を使用します。
`dkim_sign_headers` は profile 内の `headers` を上書きしません。
認証済みの domain に profile がない場合は、既定で配送をエラーにして再送対象とします。
`unknown_domain: unsigned` は、その未知ドメインに限って unsigned を明示許可する設定です。
既知ドメインの署名器不在・鍵不正を unsigned に変える設定ではありません。

起動前の `-check-config` は JSON・鍵形式・algorithm・domain/selector/header を検証します。
この検証は DNS へ接続しません。起動時と SIGHUP の反映時は全 active profile の
`selector._domainkey.domain` を10秒の全体期限内に照会し、公開鍵との一致を検証します。
1つの TXT RR、非空の `p`、algorithm、SHA-256 / email の制約を確認します。
DNS 未公開、鍵不一致、重複 TXT、失効した空の `p`、不正 JSON、読めない鍵では反映しません。
起動時に有効な旧設定がない場合は起動を停止します。

この確認は設定済み resolver から見た公開状態です。全受信側への DNS キャッシュ伝播や
DNSSEC 検証を保証しません。DNS 事前公開と待機は管理者が行います。
形式の基準は [RFC 6376](https://www.rfc-editor.org/rfc/rfc6376.html) と
[Ed25519 の RFC 8463](https://www.rfc-editor.org/rfc/rfc8463.html) です。

## 切替と rollback

1. 新しい selector とバージョン付き鍵ファイルを用意し、専用ユーザーからだけ読める権限にする。
   既存の鍵を直接上書きしない。同じ active selector で公開鍵を変える更新は拒否する。
2. 新 selector の公開鍵を DNS へ追加する。旧 selector の公開鍵は残す。
   想定する受信側の resolver で確認し、DNS TTL とキャッシュ伝播の待機期間を確保する。
3. domain・新 selector・algorithm・新 key_file を JSON に一緒に記載し、revision を更新する。
   一時ファイルから rename して配置し、`kuroshio -check-config -config ...` を実行する。
4. SIGHUP を送る。`config_reloaded` / `config_reload_rejected` と `config_generation` を確認する。
   不正更新では旧 profile・ログレベル・世代を維持する。
5. 新しい配送の署名を公開鍵で検証する。切替前に署名用の組を取得済みの処理は旧 selector を使う。
   キュー内の未署名データや再送は、次の署名時点の active profile を使う。

鍵は反映時に読み込んで保持し、署名中にファイルから別の鍵を読み直しません。
複数ドメインの profile は一括で切り替え、DKIM/ARC が別世代の鍵を使わないようにします。
ARC の既存 chain は従来どおり暗号学的に検証し、入力ヘッダの検証結果を信用しません。

旧 selector の DNS は、旧署名を持つメールが下流のキュー・転送経路を通過し終わるまで保持します。
本 MTA の `max_retry_age` だけでは、下流の配送期間を上限付けできません。
受信側・転送先の保持期間、DNS TTL、運用上の余裕を含めて退役期限を決め、
無期限に滞留し得る構成では配送完了を確認するまで公開鍵を残してください。
rollback 期間中は旧秘密鍵も保護して保持します。旧 manifest を再配置して SIGHUP し、
旧 DNS と鍵の一致を再確認して戻します。selector を別の鍵へ再利用しないでください。

## 既存キューからの移行

profile モードを有効にする前に、従来の非 null sender のキューを排出してください。
旧 JSON に署名権限がない場合、ヘッダから権限を推測せず、profile モードの配送をエラーにします。
旧メッセージは読み込めますが、設定を従来モードへ戻して配送するか、認可された投入経路から
運用上の重複を確認して再投入する必要があります。
新しい内部メタデータは local/Kafka の JSON と再送・再起動で保持します。
queue ディレクトリと Kafka topic の producer 権限・接続元を信頼境界として保護し、
任意の外部 producer から内部 JSON を直接投入しないでください。

鍵・JSON 内の秘密情報をログやコミットへ含めないでください。
鍵の退役・削除は rollback 期間の終了後に運用側で行います。
自動 DNS 更新、鍵生成、KMS/HSM 連携、信頼 CIDR や HTTP API 用の追加署名認可はこの段階では提供しません。
