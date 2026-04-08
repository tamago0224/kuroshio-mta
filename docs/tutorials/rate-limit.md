# Rate Limit を試す

YAML でルールを書き、`kuroshio-mta` の受信レート制限がどう効くかを確かめるハンズオンです。
このページでは `docker compose` で最小環境を立ち上げて、実際に制限へ到達するところまで試します。

## 前提

- [Getting Started](/getting-started) を完了している
- まずは `rate_limit_backend: memory` で試す
- 複数ノード構成を試したい場合だけ Redis / Valkey を使う

## 1. tutorial 用の compose を起動する

この tutorial 用に、最小設定の `compose.yaml` と `config.yaml` を
`examples/tutorials/rate-limit/` に用意しています。

```bash
mkdir -p examples/tutorials/rate-limit/var/queue
docker compose -f examples/tutorials/rate-limit/compose.yaml up --build -d
```

設定フォーマットの詳細は [Rate Limit 設定](/rate_limit) を参照してください。

この最小構成では次のルールを使っています。

- `ingress_rate_limit_per_minute: 10`
- `rate_limit_backend: memory`
- `rate_limit_rules: "connect:ip:3:1m;mailfrom:ip+mailfrom:2:1m"`

## 2. 制限を超えるように接続する

短時間に複数回 SMTP セッションを張ると、接続元 IP や `MAIL FROM` 単位のルールに達します。

まずは接続回数制限を超える最小例を試します。

```bash
for i in 1 2 3 4; do
  docker compose -f examples/tutorials/rate-limit/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 2525
EHLO localhost
QUIT
EOF"
done
```

4 回目以降で `421` や `451` 系の拒否応答になれば、`connect:ip:3:1m` が効いています。

`MAIL FROM` 単位のルールを見たい場合は、同じ送信者で複数回投げます。

```bash
for i in 1 2 3; do
  docker compose -f examples/tutorials/rate-limit/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 2525
EHLO localhost
MAIL FROM:<sender@example.net>
RSET
QUIT
EOF"
done
```

## 3. Observability を確認する

```bash
curl http://127.0.0.1:9090/metrics | head
```

## 4. Redis / Valkey に切り替える

複数ノードで共有したい場合は、状態だけ Redis / Valkey に外部化できます。

```yaml
rate_limit_backend: redis
rate_limit_redis_addrs:
  - localhost:6379
rate_limit_redis_key_prefix: kuroshio:ratelimit
```

この tutorial は `memory` backend 前提ですが、設定だけ `redis` に差し替えれば拡張できます。

## 5. 後片付け

```bash
docker compose -f examples/tutorials/rate-limit/compose.yaml down
rm -rf examples/tutorials/rate-limit/var
```

## 6. どこまで外部化されるか

現在 Redis / Valkey に出しているのは RateLimiter のヒット状態だけです。

- RateLimiter 状態: 外部化対象
- DNSBL キャッシュ: 外部化対象外
- 配送側 throttle: 外部化対象外

## 次に読むページ

- 詳細設定: [Rate Limit 設定](/rate_limit)
- 負荷試験: [Load / Chaos](/runbooks/load_chaos)
- Admin API: [Admin API を試す](/tutorials/admin-operations)
