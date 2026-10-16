# HTTP ポリシーフックを試す

独自 API `kuroshio.smtp-policy.v1` の accept / reject / tempfail をローカルのモックで確認します。
Stalwart MTA Hooks の仕様を採用したものではなく、API 互換はありません。
本文改変・宛先変更・スクリプト実行・Milter は対象外です。

## 起動

リポジトリルートから、別々のターミナルで実行します。Python 3 と Go が必要です。
既存設定をコピー・上書きしません。2525 / 9081 / 9090 が使用中の場合は設定とモックの `--port` を変更してください。

```bash
python3 examples/tutorials/policy-hook/mock_policy.py
```

```bash
go run ./cmd/kuroshio -config ./examples/tutorials/policy-hook/config.yaml
```

設定例は loopback のみで受信・HTTP通信し、配送後のメールを `./var/policy-hook-spool` に保存します。
`allow_http_loopback: true` はこの検証用です。本番環境は管理者が信頼する HTTPS endpoint と検証済み証明書を使ってください。
モックはローカル検証専用で、認証やスパム・ウイルス検査は実装していません。

## SMTP 応答を確認する

```bash
python3 - <<'PY'
import smtplib

for recipient in ["accept@recipient.test", "reject@recipient.test", "tempfail@recipient.test", "data-reject@recipient.test"]:
    with smtplib.SMTP("127.0.0.1", 2525, timeout=10) as smtp:
        smtp.ehlo()
        smtp.mail("sender@sender.test")
        code, reply = smtp.rcpt(recipient)
        print(recipient, "RCPT", code)
        if code == 250:
            try:
                code, reply = smtp.data("From: sender@sender.test\r\nSubject: hook demo\r\n\r\nhello\r\n")
            except smtplib.SMTPDataError as error:
                code = error.smtp_code
            print(recipient, "DATA", code)
PY
```

期待値:

| recipient | RCPT | DATA |
| --- | --- | --- |
| `accept@recipient.test` | 250 | 250 |
| `reject@recipient.test` | 550 | 未実行 |
| `tempfail@recipient.test` | 451 | 未実行 |
| `data-reject@recipient.test` | 250 | 554 |

モック停止中は RCPT が451になります。受理済みメールをこの理由で削除する処理はありません。
`curl --fail http://127.0.0.1:9090/metrics` で `smtp_policy_hook_` カウンタを確認します。
終了時は両ターミナルで Ctrl-C を押します。キューや spool は保存したままです。

## JSON schema と送信内容

[request schema](../../examples/tutorials/policy-hook/request.schema.json) と
[response schema](../../examples/tutorials/policy-hook/response.schema.json) を参照してください。
リクエストには `version`、`stage`、`remote_ip`、`helo`、`mail_from`、`recipients`、`tls`、
`submission`、`authenticated`、`message_bytes` を含めます。
RCPT では `recipients` はそれまでの受理済み宛先、`recipient` は今回の候補です。
DATA では全受理済み宛先と最終メッセージのバイト数を含めます。

既定では envelope と接続情報のみを送ります。`send_body: true` にすると DATA でのみ
`message_base64` に追加ヘッダ込みの最終メッセージを送ります。RCPT では本文を送信しません。
SMTP AUTH のシークレットや認証ユーザー名は常に除外します。モックはリクエストのログを出しません。

最小の応答は次の通りです。

```json
{"version":"kuroshio.smtp-policy.v1","action":"accept"}
```

`reject` は恒久拒否、`tempfail` は再送可能な一時失敗です。HTTP エラーや不正応答は tempfail になります。
フックは既存拒否条件を通過した後で呼ばれるため、accept で組み込みの認証・メール認証ポリシーを迂回できません。
タイムアウト、同時数、TLS、secret、監視の詳細は [Configuration](../configuration.md#smtp-http-ポリシーフック) を参照してください。
