# TCP LB と PROXY v2

専用 listener と trusted proxy CIDR を併用して、LB が伝達する client IP を採用します。
[設定](../configuration.md#信頼した-tcp-proxy-と-proxy-v2) を先に確認してください。

## 信頼境界

MTA の backend ポートへ接続できる送信元を firewall / security group でも LB だけに限定します。
CIDR は実際の backend TCP peer に合わせ、全インターネットや共有ネットワーク全体を許可しません。
信頼した LB は任意 IP を伝達できるため、設定を変更できる管理者も同じ信頼境界に含まれます。
LB が正しい接続元を記録・伝達する責任を持ちます。

LB は TCP passthrough とし、クライアントごとに backend 接続を作ります。
別クライアントへの接続使い回しは避けてください。TLS を MTA まで通し、
STARTTLS と暗黙 TLS の両方で PROXY ヘッダを TLS bytes より先に送ります。
TLS 終端 LB の TLS TLV や HTTP X-Forwarded-For により AUTH 要件を解除する機能はありません。

HAProxy の backend 設定例（アドレスとポートは環境に合わせます）:

```text
backend kuroshio_smtp
    mode tcp
    server mta 192.0.2.20:2525 send-proxy-v2
```

LB の health check も有効な v2 ヘッダを送るか、別の observability endpoint を使います。
LOCAL コマンドでは client アドレスを採用せず、通常の TCP peer として制限・認可します。
無効な listener に v2 ヘッダを送っても接続元の上書きは行いません。

形式の根拠は [HAProxy PROXY protocol specification](https://www.haproxy.org/download/2.9/doc/proxy-protocol.txt) です。
本実装は TCP IPv4/IPv6 の利用に範囲を限定し、LOCAL、長さ上限、TLV の扱いは設定文書に記載しています。

## 検証

ローカル TCP の小さな LB を介した結合テストを用意しています。

```bash
go test -race -tags=integration ./internal/smtp -run '^TestProxyV2LoopbackLB$' -v
```

SMTP・STARTTLS Submission・暗黙 TLS Submission を通して、SPF に渡す IP、DNSBL query、
IP rate limit の bucket、Received が伝達 client IP を使うことを検証します。
実際の HAProxy 製品との結合や本番 LB の到達制御は別途環境で確認してください。

```bash
go test -race ./internal/smtp -run '^TestProxy' -v
```

単体テストでは IPv4/IPv6、信頼外、不正ヘッダ、過大・途中フレーム、timeout、キャンセル、
後続データの保持、TLS TLV が平文 AUTH を許可しないことを確認します。

本番展開前に LB 経由の1通を使い、Received と SPF の結果を確認します。
さらに信頼外ホストから backend 接続が拒否されること、直接接続 listener の既存動作、
Submission の TLS と認証要件を確認してください。
中継権限は client IP / 認証に基づき、信頼 LB の指定だけで付与しません。

## 監視と復旧

- `smtp_proxy_accepted_total`: 有効な PROXY / LOCAL フレームを受け付けた接続数。
- `smtp_proxy_reject_untrusted_total`: TCP peer が信頼外。
- `smtp_proxy_reject_read_total`: timeout、キャンセル、EOF、読取り失敗。
- `smtp_proxy_reject_invalid_total`: 形式・アドレス・TLV が不正、または対象外の形式。
- `smtp_proxy_reject_oversized_total`: payload 長が4096バイトを超過。

カウンターは有効 listener 全体の合計です。IP・port・TLV の内容をメトリクスに含めず、
PROXY の失敗でフレーム本文をログに記録しません。
通常の SMTP ログと tracing は既存の PII マスキング設定を維持してください。

拒否の増加時は、実 TCP peer の CIDR、LB の v2 送信設定、backend の listener 選択、
TLS より前のフレーム送信、5秒以内の到達を確認します。
復旧のために信頼範囲を広げず、LB と MTA の設定を揃えてください。
直接接続へ戻す場合は LB 側のヘッダ送信と listener の設定を両方切り替え、MTA を再起動します。
