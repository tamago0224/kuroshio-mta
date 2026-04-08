# Rate Limit を試す

YAML でルールを書き、`kuroshio-mta` の受信レート制限がどう効くかを確かめるハンズオンです。

## 前提

- [Getting Started](/getting-started) を完了している
- まずは `rate_limit_backend: memory` で試す
- 複数ノード構成を試したい場合だけ Redis / Valkey を使う

## 1. ルールを書く

まずは `config.yaml` に最小のルールを入れます。

```yaml
ingress_rate_limit_per_minute: 10
rate_limit_backend: memory
rate_limit_rules: "connect:ip:3:1m;mailfrom:ip+mailfrom:2:1m"
```

設定フォーマットの詳細は [Rate Limit 設定](/rate_limit) を参照してください。

## 2. MTA を起動する

```bash
go run ./cmd/kuroshio -config ./config.yaml
```

## 3. 制限を超えるように接続する

短時間に複数回 SMTP セッションを張ると、接続元 IP や `MAIL FROM` 単位のルールに達します。

負荷をかけて挙動を見るなら、既存の load script が使えます。

```bash
scripts/load/run_smtp_load.sh normal 127.0.0.1:2525
```

## 4. Redis / Valkey に切り替える

複数ノードで共有したい場合は、状態だけ Redis / Valkey に外部化できます。

```yaml
rate_limit_backend: redis
rate_limit_redis_addrs:
  - localhost:6379
rate_limit_redis_key_prefix: kuroshio:ratelimit
```

## 5. どこまで外部化されるか

現在 Redis / Valkey に出しているのは RateLimiter のヒット状態だけです。

- RateLimiter 状態: 外部化対象
- DNSBL キャッシュ: 外部化対象外
- 配送側 throttle: 外部化対象外

## 次に読むページ

- 詳細設定: [Rate Limit 設定](/rate_limit)
- 負荷試験: [Load / Chaos](/runbooks/load_chaos)
- Admin API: [Admin API を試す](/tutorials/admin-operations)

