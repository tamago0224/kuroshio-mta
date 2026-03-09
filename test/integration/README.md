# DNS Integration Environment

`docker compose` で DNS/TXT/TLSA を持つ検証環境を起動し、`mailauth` と送信側ポリシーを結合テストできます。

## 構成

- `dnsmock`: TXT/MX/A/TLSA を返す軽量DNSサーバ
- `policy`: MTA-STS policy 配信用HTTPサーバ
- `tester`: `go test -tags=integration` を実行するコンテナ

## 任意レコードの追加

`dns/records.json` を編集します。

- SPF: `txt[].name=example.test`, `texts=["v=spf1 ..."]`
- DMARC: `txt[].name=_dmarc.example.test`
- DANE: `tlsa[].name=_25._tcp.<mx-host>`

## 実行

```bash
./scripts/integration/run_dns_env_tests.sh
```

## 備考

- DANE テスト向けに `dnsmock` は応答で AD ビットを立てます。
- MTA-STS は `policy` サービス配信の policy ファイルを読み取って検証します。
