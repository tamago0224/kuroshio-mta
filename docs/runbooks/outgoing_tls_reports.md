# Outgoing TLS-RPT

RFC 8460 に基づき、kuroshio-mta が外向き配送で観測した MTA-STS / DANE TLS 結果を宛先運用者へ集約通知する機能です。既定では無効です。

## 有効化

```yaml
tls_report:
  enabled: true
  organization: "Example Operations"
  contact_info: "postmaster@example.com"
  report_domain: "mail.example.com"
  flush_interval: 1h
  max_records: 100000
  max_report_bytes: 5242880
```

状態は `queue_dir/tls-report-state.json` に mode `0600` で保存します。recipient、message ID、本文、認証情報は保存しません。完了済みの UTC 日だけを対象にし、HTTP 2xx を受け取って状態を永続更新した後に集約を削除します。接続断や非2xxでは保持し、次の interval に再送します。report ID は report domain と期間から安定生成されるため、receiver は重複排除に利用できます。

## DNS と transport

宛先 policy domain の `_smtp._tls` TXT を検索します。`v=TLSRPTv1;` で始まる record がちょうど1件の場合だけ使用し、`rua` のうち最初の有効な `https:` URIへ送信します。

- `mailto:` と未知 scheme は初期実装では送信しない
- redirect は追跡しない
- HTTPS 証明書検証を無効化しない
- environment proxy を使わない
- DNS 解決結果に loopback、private、link-local、unspecified、multicast が含まれる endpoint は拒否する
- response body は最大4 KiBだけ破棄する

RFC 8460 は HTTPS 証明書エラーを無視する余地を認めていますが、この実装は DNS由来 endpoint への外部送信という信頼境界を優先し、検証を維持します。

## 対応範囲

初期段階では、MTA-STS enforce / DANE policy がある MX attempt を集約します。MTA-STS testing mode を成功heartbeatとして数えません。`no-policy-found` の opportunistic TLS 成否、MTA-STS policy fetch 自体の失敗、`mailto:` transport、gzip、複数 report への自動分割は対象外です。上限を超えた report は送信せず状態を保持します。

failure reason は control character を除去して256 byteに制限します。主な分類は `starttls-not-supported`、証明書関連、`validation-failure`、`dns-error`、`connection-failure` です。

## 監視

- `tls_report_observation_dropped_total`: 観測channel上限による破棄
- `tls_report_store_errors_total`: 永続集約失敗
- `tls_report_dns_errors_total`: TLSRPT TXT lookup失敗
- `tls_report_delivery_retried_total`: HTTPS送信失敗または非2xx
- `tls_report_delivered_total`: report送信と状態削除の成功
- `tls_report_flush_errors_total`: report生成・状態更新失敗

保持数が継続的に増える場合は TXT policy、endpoint証明書、DNS解決先、report size 上限を確認します。

## 通知ループ

TLS-RPT 自身を SMTP queue へ投入しない HTTPS-only 設計のため、report配送は外向き SMTP 観測に再入力されません。将来 `mailto:` を追加する場合は、report配送を観測対象から除外し、DKIMの `s=tlsrpt` と TLS policy bypass を専用経路で扱う必要があります。
