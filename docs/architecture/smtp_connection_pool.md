# SMTP connection pool

## 目的と保証範囲

送信側 SMTP 接続プールは、小さいメールを同じ配送経路へ連続送信するときの TCP 接続、EHLO、STARTTLS、relay AUTH の反復を減らします。既定は無効です。並列 PIPELINING、複数 recipient の一括 SMTP transaction、プロセス間での接続共有は対象外です。

接続は destination、送信元 IP/EHLO、relay 認証 identity、TLS 必須条件、DANE TLSA 条件ごとに分離します。1接続を同時に使う goroutine は1つだけです。pool の上限待ちは呼び出し元 context のキャンセルに従い、`Client.Close` 後に返却された接続は保持しません。

## 障害時の扱い

- 421、EOF、読み書き失敗、TLS/AUTH 失敗の接続は再利用しません。
- MAIL、RCPT、DATA 開始前の SMTP reject は RSET が成功した場合だけ再利用します。
- DATA 本文と終端を送った後に最終応答を読めない場合、配送済みか判断できません。エラーを `DeliveryStatusUnknownError` として明示し、接続を破棄してメッセージを DLQ に移します。重複を避けるため自動再送は行わず、運用者が相手側の受理状況を確認してから再投入を判断します。
- idle timeout、最大寿命、最大配送回数に達した接続は次回 checkout または返却時に閉じます。常駐 cleanup goroutine は使用しません。

## 計測

同一 host へ十分な件数を送る代表 workload で、無効時と有効時を別プロセスとして比較します。送信件数・本文サイズ・worker 数・TLS 設定を揃え、warm-up 後に複数回計測してください。

確認項目:

- `smtp_pool_dialed_total` と `smtp_pool_reused_total` による接続数と再利用率
- `smtp_pool_tls_handshakes_total` による TLS handshake 回数
- 配送成功数と workload 全体の経過時間から算出する throughput / 平均配送時間
- `smtp_pool_open_connections` と `smtp_pool_idle_connections` が設定上限以下であること
- 終了後に接続と goroutine が残らないこと、`go test -race ./internal/delivery` が成功すること

loopback の平文 SMTP はネットワークや TLS の本番効果を代表しません。採用判断では実際の relay/MX に近い RTT と TLS を使い、単一結果ではなく複数回の分布を比較します。

### 初期実装の回帰ベンチマーク

2026-10-23 に `BenchmarkSMTPConnectionReuse` を Linux/AMD Ryzen 7 5700X、平文 loopback、各200配送、3回で実行しました。

| mode | latency | connections/op | allocations |
| --- | --- | --- | --- |
| pool disabled | 330–351 µs/op | 1.000 | 118 allocs/op |
| pool enabled | 64–67 µs/op | 0.005 | 50 allocs/op |

この結果は、同一キーで1接続を200配送に再利用でき、接続上限ロジックのオーバーヘッドが都度接続より小さいことを確認する回帰用の値です。TLS handshake 時間、外部ネットワーク RTT、相手サーバーの idle 切断は含まないため、本番容量計画にはそのまま使用しません。

再実行コマンド:

```bash
go test ./internal/delivery -run '^$' -bench '^BenchmarkSMTPConnectionReuse$' -benchtime=200x -count=3 -benchmem
```
