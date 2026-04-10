# Domain Throttle を観測する

配送側 `domain throttle` が効いている様子を、Redis / Valkey backend と metrics / logs で確認する tutorial です。

この tutorial では次をまとめて試します。

- 同じ宛先ドメインへの配送を 1 並列に絞る
- その結果として wait が発生することを metrics で確認する
- Redis を止めた時に fail-open で処理が続き、backend error が記録されることを確認する

## 前提

- Docker と `docker compose` が使える
- ローカルで `:2525`、`:2526`、`:6379`、`:9090` を使える

使う compose 一式は [examples/tutorials/domain-throttle-observability](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/domain-throttle-observability) にあります。

## 起動するもの

- `kuroshio`: tutorial 用設定で起動する `kuroshio-mta`
- `redis`: domain throttle backend
- `slow-smtp`: 3 秒遅れて受け付けるテスト用 SMTP 宛先
- `smtp-client`: SMTP セッション投入用

## 1. compose を起動する

```bash
docker compose -f examples/tutorials/domain-throttle-observability/compose.yaml up --build -d
```

この tutorial では次の設定を使います。

- `delivery_mode: relay`
- `relay_host: slow-smtp`
- `domain_max_concurrent_default: 1`
- `domain_throttle_backend: redis`
- `domain_adaptive_throttle: false`

## 2. 同じドメイン宛てに複数通を並列投入する

```bash
docker compose -f examples/tutorials/domain-throttle-observability/compose.yaml exec -T smtp-client sh -lc '
for i in 1 2 3 4; do
  cat <<EOF | nc kuroshio 2525 >/dev/null &
EHLO tutorial.local
MAIL FROM:<sender@example.com>
RCPT TO:<user${i}@example.net>
DATA
Subject: domain throttle ${i}

hello ${i}
.
QUIT
EOF
done
wait
'
```

`worker_count` は 4 ですが、宛先ドメインはどれも `example.net` なので、
配送は 1 並列ずつしか進まないはずです。

`worker_domain_throttle_wait_ms_total` をはっきり増やしたいので、
この step では逐次ではなく並列に SMTP セッションを流します。

## 3. metrics を見る

```bash
curl http://127.0.0.1:9090/metrics | grep worker_domain_throttle
```

ホスト側から見づらい環境では、compose network の中から見ても構いません。

```bash
docker compose -f examples/tutorials/domain-throttle-observability/compose.yaml exec -T smtp-client sh -lc \
  "wget -qO- http://kuroshio:9090/metrics | grep worker_domain_throttle"
```

特に次を見ます。

- `worker_domain_throttle_acquire_total`
- `worker_domain_throttle_wait_ms_total`
- `worker_domain_throttle_backend_error_total`

`worker_domain_throttle_wait_ms_total` が増えていれば、
同じドメインへの配送が待たされていることを確認できます。

## 4. ログを見る

```bash
docker compose -f examples/tutorials/domain-throttle-observability/compose.yaml logs kuroshio
```

正常系では大きなエラーは出ません。
wait 自体は metrics で見るのが分かりやすいです。

## 5. fail-open を試す

Redis / Valkey backend が落ちても配送を止めずに進めるのが、現在の実装方針です。

まず Redis を止めます。

```bash
docker compose -f examples/tutorials/domain-throttle-observability/compose.yaml stop redis
```

その後、もう 1 通投入します。

```bash
docker compose -f examples/tutorials/domain-throttle-observability/compose.yaml exec -T smtp-client sh -lc "cat <<'EOF' | nc kuroshio 2525
EHLO tutorial.local
MAIL FROM:<sender@example.com>
RCPT TO:<fallback@example.net>
DATA
Subject: redis fallback

hello fallback
.
QUIT
EOF"
```

再度 metrics とログを見ます。

```bash
curl http://127.0.0.1:9090/metrics | grep worker_domain_throttle_backend_error
docker compose -f examples/tutorials/domain-throttle-observability/compose.yaml logs kuroshio
```

ここで `worker_domain_throttle_backend_error_total` が増えていれば、
backend error を記録しつつ fail-open で進んでいることが分かります。

## 6. OTEL tracing と組み合わせる場合

この tutorial 自体は tracing stack を起動しませんが、
OpenTelemetry を有効化している構成では次の event も trace 上で見られます。

- `domain_throttle.acquire`
- `domain_throttle.backend_error`

trace の確認方法は [OTEL Tracing を試す](/tutorials/otel-tracing) を参照してください。

## 7. 後片付け

```bash
docker compose -f examples/tutorials/domain-throttle-observability/compose.yaml down
rm -rf examples/tutorials/domain-throttle-observability/var
```

## 関連

- [Rate Limit 設定](/rate_limit)
- [Observability](/observability)
- [Domain Throttle Externalization](/architecture/domain_throttle_externalization)
- [Tutorials Home](/tutorials/)
