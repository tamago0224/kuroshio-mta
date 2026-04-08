# Tutorials

`kuroshio-mta` の主要機能を、手元で実際に動かしながら確認するための入口です。

ほとんどの tutorial は `docker compose` で最小環境を起動し、SMTP・Admin API・DNS・MTA-STS policy などをそのまま確認できるようにしています。

## まずどこから試すか

| Tutorial | 何を確認するか | 主な環境 |
| --- | --- | --- |
| [最小メールフローを試す](/tutorials/basic-mail-flow) | SMTP で 1 通受信し、local queue に入るところ | `kuroshio` + `smtp-client` |
| [Rate Limit を試す](/tutorials/rate-limit) | 接続元 IP や `MAIL FROM` 単位の制限 | `kuroshio` + `smtp-client` |
| [Admin API を試す](/tutorials/admin-operations) | queue 操作と Admin API / CLI | `kuroshio` + `smtp-client` |
| [メール認証を試す](/tutorials/mail-auth) | SPF / DMARC 評価、DKIM / ARC 署名 | `CoreDNS` + `policy` + `tester` |
| [TLS 配送ポリシーを試す](/tutorials/tls-policy) | STARTTLS、MTA-STS、DANE | `CoreDNS` + `policy` + `tester` |

## compose ベースの tutorial 環境

- 基本 SMTP: [examples/tutorials/basic-mail-flow](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/basic-mail-flow)
- Rate Limit: [examples/tutorials/rate-limit](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/rate-limit)
- Admin API: [examples/tutorials/admin-operations](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/admin-operations)
- DNS / Web: [examples/tutorials/dns-services](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/dns-services)

## 進み方のおすすめ

1. [Getting Started](/getting-started) で最初の起動方法を確認する
2. [最小メールフローを試す](/tutorials/basic-mail-flow) で 1 通受ける
3. [Rate Limit を試す](/tutorials/rate-limit) と [Admin API を試す](/tutorials/admin-operations) で運用系を見る
4. [メール認証を試す](/tutorials/mail-auth) と [TLS 配送ポリシーを試す](/tutorials/tls-policy) で DNS / policy 系を確認する
