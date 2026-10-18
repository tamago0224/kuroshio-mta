# TLS 証明書の更新

SMTP STARTTLS、Submission STARTTLS、暗黙 TLS Submission は、起動時に
`tls_cert_file` / `tls_key_file` を読み、稼働中は各 listener が1分ごとに再読込します。
追加設定・再起動・signal は不要です。パスの変更には再起動が必要です。
更新は listener ごとに独立して反映されるため、全 listener の切替が同時になる保証はありません。

## 検証と切替

PEM の解析、秘密鍵と leaf 証明書の一致、含まれる全証明書の有効期間を確認してから、
新しい証明書チェーンと鍵を一組としてメモリに公開します。
不正 PEM、書込み途中、ファイル欠落、鍵不一致、期限切れ・有効期間前は更新を拒否します。
起動時に不正な場合は listener を開始しません。
CA の信頼性、ホスト名、失効情報の検証はこの再読込の対象外です。
配置する管理者がチェーン・SAN・serverAuth 用途を事前に確認してください。

切替後に TLS handshake を開始する接続は新しい証明書を使います。
既存 TLS 接続は切断せず継続します。handshake 進行中はその時点で取得した証明書を使います。
セッション再開では証明書交換が省略されることがあるため、確認は新しいクライアント接続で行います。
更新失敗時は直前の証明書を保持し、警告とエラーカウンターを記録します。
保持中の証明書が失効しても平文への切替は行いません。クライアントの証明書検証が失敗するため、期限前の監視が必要です。

## 外部 ACME client からの配置

本体は CA 通信や DNS provider credential の管理を行いません。
外部 Certbot 等で取得し、成功した更新の deploy hook で専用ディレクトリへ配置します。
専用ユーザーから読める所有者・グループとディレクトリ権限を設定し、秘密鍵は0640以下にします。
コンテナでは単一ファイルの bind mount ではなく、証明書ディレクトリを read-only mount してください。
単一ファイル mount はファイルの置換を追従しないことがあります。

証明書と鍵を直接上書きせず、新しいバージョンのディレクトリへ両方を書き、
検証後に `current` symlink を同じファイルシステム上の rename で置換します。
例えば設定を次の固定パスにします。

```yaml
tls_cert_file: /etc/kuroshio/tls/current/fullchain.pem
tls_key_file: /etc/kuroshio/tls/current/privkey.pem
```

deploy hook は `$RENEWED_LINEAGE/fullchain.pem` と `privkey.pem` を管理者が用意した
新バージョンへコピーし、権限と鍵ペアを検証してから `current` を置換します。
切替の瞬間に別バージョンの2ファイルを読み取っても、鍵が一致しなければ前の証明書を保持して次の周期で再試行します。
旧バージョンは反映確認まで残し、復旧時は symlink を戻してください。
秘密鍵や PEM の内容を hook のログ・シェル trace に出さないでください。

Certbot の更新検証例:

```bash
certbot renew --dry-run
certbot renew --deploy-hook /usr/local/sbin/deploy-kuroshio-tls
```

`--dry-run` は staging CA を使用します。通常 deploy hook は実行しません。
hook を含む検証は隔離環境で `--dry-run --run-deploy-hooks` を指定します。
この場合 hook に渡るのは現行の有効証明書です。staging 証明書の実際の反映を試す場合は、
別の Certbot config/work/log ディレクトリで `certonly --staging` を使い、隔離した MTA にだけ配置します。
staging CA は通常の trust store で信頼されません。本番証明書を staging 証明書で置換しないでください。
取得 challenge の種類・DNS credential・更新 timer は外部 client の公式手順に従って設定します。
[Certbot User Guide](https://eff-certbot.readthedocs.io/en/stable/using.html) を参照してください。

## 反映確認と監視

1分以上待ち、各有効 listener と各ノードについて新しい証明書の serial・期限を確認します。

```bash
openssl s_client -starttls smtp -connect mail.example.com:25 -servername mail.example.com </dev/null 2>/dev/null | openssl x509 -noout -serial -enddate
openssl s_client -starttls smtp -connect mail.example.com:587 -servername mail.example.com </dev/null 2>/dev/null | openssl x509 -noout -serial -enddate
openssl s_client -connect mail.example.com:465 -servername mail.example.com </dev/null 2>/dev/null | openssl x509 -noout -serial -enddate
```

これらは提示内容の確認です。CA/ホスト名の検証にはクライアント側で
`-verify_return_error -verify_hostname mail.example.com` も指定してください。

`/metrics` で以下の prefix ごとに監視します。

| prefix | 対象 |
| --- | --- |
| `smtp_tls` | SMTP STARTTLS |
| `submission_tls` | Submission STARTTLS |
| `submission_implicit_tls` | 暗黙 TLS Submission |

`<prefix>_certificate_not_after_seconds` は現在メモリで保持する leaf の失効 UNIX 時刻です。
接続がなくても公開するため、例えば
`smtp_tls_certificate_not_after_seconds - time() < 14 * 86400` を通知条件にできます。
`<prefix>_certificate_reload_total` は変更反映数、
`<prefix>_certificate_reload_error_total` は失敗した周期数です。
失敗は1分ごとに警告します。ファイル内容や鍵をログへ記録しません。
無効な listener や TLS 未設定の listener はこれらの gauge を公開しません。
エラー増加時は権限・PEM・鍵の一致・期限を確認し、復旧後に全 listener の serial を再確認してください。
