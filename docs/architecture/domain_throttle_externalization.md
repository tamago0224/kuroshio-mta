# Domain Throttle State Externalization

## 背景

`kuroshio-mta` の配送側 `domain throttle` は、現在 `internal/worker/domain_throttle.go` の
`perDomain` マップに状態を持っています。

この実装でローカルに保持しているものは次の 3 つです。

- ドメインごとの同時実行数を制御する semaphore
- adaptive throttle 用の penalty
- penalty 更新用の sample / tempfail 集計

単一ノードでは十分ですが、複数ノードで worker を並べると次の問題が残ります。

- 同じ宛先ドメインに対する同時配送数をノード間で共有できない
- 一時失敗率から計算する penalty がノードごとにばらつく
- ノード障害時に semaphore の占有状態が見えない

PR #184 で ingress 側の RateLimiter 状態だけ Redis / Valkey に外部化したため、
次は配送側 throttle も同様に複数ノードで一貫した挙動を取れるようにするのが自然です。

## 目標

- 複数 worker ノードでドメインごとの同時実行上限を共有する
- adaptive throttle の penalty 計算をノード横断でそろえる
- 外部ストア障害時の挙動を明確にする
- metrics / logs / traces から状態を追えるようにする

## 実装状況

現在の `kuroshio-mta` では、`domain_throttle_backend: redis` を使うことで
Redis / Valkey に次を共有できます。

- ドメインごとの同時実行 lease
- adaptive throttle の sample / tempfail 集計
- penalty 状態

実行中の backend エラーは fail-open で扱い、warn ログを出しつつローカル処理へフォールバックします。
一方で、起動時に `domain_throttle_backend: redis` の接続初期化に失敗した場合は起動エラーです。

## 非目標

- 初回で delivery policy 全体を Redis 化しない
- 宛先ごとの全文メタデータ保存や長期履歴保存を行わない
- domain throttle と ingress RateLimiter を 1 つの抽象に無理に統合しない

## 現状の問題を分解する

### 1. 同時実行数

いまは `chan struct{}` のバッファサイズでドメインごとの同時実行数を制御しています。
これはプロセス内では軽量ですが、別ノードから見えません。

### 2. adaptive penalty

`samples` と `tempFail` が 20 件たまったところで、tempfail 比率に応じて penalty を更新しています。
この集計もプロセスローカルなので、同じドメインでもノードごとに異なる penalty になります。

### 3. 障害時整合性

worker が途中で死ぬと、そのノードのメモリだけが失われます。
外部化する場合は逆に、lease が残り続けないよう TTL を考える必要があります。

## 推奨方針

第 1 候補は `Redis / Valkey` です。

理由:

- ingress 側の RateLimiter ですでに採用している
- 単一ノードと複数アドレスの両方を扱える実装がある
- lease、counter、TTL ベースの制御と相性が良い
- フェイルオープン / フェイルクローズを切り替えやすい

## 提案アーキテクチャ

### 保存先

- backend 名: `redis`
- 主用途:
  - 同時実行スロットの lease 管理
  - adaptive throttle の集計窓
  - penalty 状態の共有

### キー設計

例:

- `kuroshio:domainthrottle:lease:<domain>`
- `kuroshio:domainthrottle:penalty:<domain>`
- `kuroshio:domainthrottle:sample:<domain>`

ハッシュ化するか平文ドメインを使うかは、運用上の可読性と key 長のバランスで決めます。
RateLimiter と揃えるなら hash 化が自然です。

### 同時実行制御

初回実装では semaphore を完全再現するより、`lease counter + TTL` を推奨します。

流れ:

1. 配送開始前に `INCR` 相当で現在の in-flight 数を増やす
2. 上限超過なら即座に戻して待機または retry する
3. 配送中は heartbeat で TTL を延長する
4. 正常終了時に `DECR` する
5. ノード障害時は TTL 切れで自然回収する

これにより、release 漏れやノード障害の影響を時間で収束させられます。

### adaptive penalty 制御

adaptive throttle は、初回は次のように単純化して共有します。

- `sample_count`
- `tempfail_count`
- `current_penalty_ms`

一定件数ごとに Lua script か transaction で更新し、

- tempfail 比率が threshold 以上なら penalty を倍増
- threshold 未満なら penalty を半減
- 上限は `domain_penalty_max`

という現在のルールを保ちます。

### local fallback

backend が `memory` の場合は現状の `domainThrottle` を維持します。

つまり当面の形は次です。

- `memory`: 既存のローカル実装
- `redis`: 複数ノード向け共有実装

## 失敗時方針

### 起動時

- `domain_throttle_backend: redis` なのに接続できない場合は起動失敗

これは ingress 側 RateLimiter と同じ方針に寄せます。

### 実行中

実行中の Redis / Valkey エラーは、初回は `fail-open` を推奨します。

理由:

- 配送停止よりも、一時的な throttle 精度低下の方が影響を抑えやすい
- mail flow を止めず、warn / trace / metrics で異常を検知できる

ただし設定で将来 `fail_closed` を選べる余地は残します。

## 観測ポイント

### metrics

追加候補:

- `domain_throttle_acquire_total`
- `domain_throttle_reject_total`
- `domain_throttle_backend_error_total`
- `domain_throttle_penalty_seconds`
- `domain_throttle_inflight`

### logs

少なくとも次を構造化ログで残します。

- backend error
- lease acquire failure
- lease release failure
- penalty update

### traces

delivery trace に次の span / event を追加できるようにします。

- `domain_throttle.acquire`
- `domain_throttle.wait`
- `domain_throttle.observe`
- `domain_throttle.release`

## 実装段階

### Phase 1

- backend 抽象を追加する
- `memory` 実装は既存ロジックをラップする
- `redis` 実装は同時実行 lease のみ共有する

### Phase 2

- adaptive penalty の集計と共有を追加する
- metrics / logs / traces を増やす

### Phase 3

- fail-open / fail-closed の切り替えを設定化する
- runbook と tutorial に複数ノード例を追加する

## 設定案

例:

```yaml
domain_throttle_backend: redis
domain_throttle_redis_addrs:
  - redis-1:6379
  - redis-2:6379
domain_throttle_redis_username: ""
domain_throttle_redis_password: ""
domain_throttle_redis_db: 0
domain_throttle_redis_key_prefix: kuroshio:domainthrottle
domain_throttle_fail_mode: open
```

命名は ingress 側 RateLimiter と揃えるのがよいです。

## 判断

`domain throttle` の状態外部化は、複数ノード構成での delivery 一貫性を上げるために価値があります。
ただし初回から adaptive penalty まで一気に完全共有するより、

1. 同時実行 lease の共有
2. adaptive penalty の共有
3. fail mode と observability の仕上げ

の順で進める方が安全です。
