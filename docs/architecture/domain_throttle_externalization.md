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

## MX / provider グループ制御

`mx_shaping.enabled: true` は SMTP 配送時の MX 集合または管理者定義 provider ごとに、
通数・TCP接続開始数の token bucket と同時接続 lease を適用します。

```yaml
mx_shaping:
  enabled: true
  backend: memory
  cache_ttl: 0s
  retry_delay: 1s
  default:
    messages_per_minute: 120
    message_burst: 10
    connections_per_minute: 60
    connection_burst: 5
    max_connections: 4
  providers:
    - name: shared-provider
      domains: [customer-a.example, customer-b.example]
      mx_suffixes: [mx.provider.example]
      limits:
        messages_per_minute: 60
        message_burst: 5
        connections_per_minute: 30
        connection_burst: 2
        max_connections: 2
```

制限と優先順位の詳細は [Configuration](../configuration.md#mx-provider-グループの配送制御) を参照してください。

### グループ判定と DNS

MX ホスト名を小文字化し、末尾のドットを除去し、重複・順序・優先度を無視した集合のハッシュを識別子にします。
同じ集合なら複数の宛先ドメインが合計上限を共有します。暗黙 MX はドメイン自身を含む集合になり、
別の暗黙 MX ドメインと自動で集約しません。共通 MX の一部だけが一致する場合も集合全体が異なれば別グループです。
プロバイダーとしてまとめたい場合は明示グループを設定してください。これは認可・組織判定には使用しません。
relay は中継ホストを集合として使います。

Go の標準 resolver は DNS RR の TTL を公開しないため、**既定はアプリケーションキャッシュなし**です。
`cache_ttl` は管理者が指定する MX スナップショットの保持時間で、DNS TTL を取得・遵守する機能ではありません。
有効期限後は再解決し、変更された集合には新しい識別子と枠を適用します。解決失敗時は期限切れ結果を再利用せず、
通常の DNS 配送失敗として再送します。失敗・空の MX 結果はキャッシュしません。
キャッシュはプロセス内・最大10,000ドメインで、満杯なら新規結果をキャッシュせず解決します。

### 実行・延期・公平性

接続直前に全上限を一括判定し、拒否した予約ではいずれの bucket も消費しません。
通数は recipient の配送試行、接続数は代替 MX を含む dial 試行を数えます。
接続枠は dial 前に取得し、接続終了後に解放します。lease の期限は dial timeout + send timeout + 30秒（最低1分）です。
全ノードのタイムアウトを揃え、接続が lease 期限を超えて継続する設定・環境では厳密な同時数保証はできません。
Redis の解放エラー時は lease 期限まで保守的に枠が残ります。

枠不足は sleep せず `NextAttempt` の更新でキューへ戻し、試行回数・既存エラー理由・DSN を変更しません。
複数 recipient のうち成功・恒久失敗・既存抑止で完了した宛先は、延期メッセージの再送対象から除きます。
実配送失敗が混在する場合は既存のメッセージ単位の再送回数・最大期間を使います。
MX 制御有効時はドメイン側の同時数・penalty も待機せず延期し、Redis エラーは fail-closed で延期します。
無効時の従来の domain throttle 動作は維持します。

別グループの due メッセージは利用可能な worker で配送できます。古いメッセージが大量にある場合でも、
延期したメッセージは未来時刻に移るため、後続スキャンで別グループへ進みます。
グループ間の厳密な帯域公平配分やテナント別公平性は提供しません。
worker のキャンセル時は新たな枠を取得せず延期します。実配送中のキャンセルは通常の配送エラー処理に従います。

ローカル・Kafka 両キューに `Defer` 契約を追加しています。永続 JSON の形式は変えず、Kafka は retry topic への
保存成功後に元の receipt を commit します。Kafka の結合検証は別途必要です。

### memory / Redis の保証範囲と障害時

- memory は1つの配送 Client 内の全 worker で共有します。プロセス再起動で枠は初期化され、複数ノード間では共有しません。
  最大10,000グループの状態を保持し、容量超過時は fail-closed で延期します。空になった bucket が完全補充される期間より前には状態を掃除しません。
- Redis は同じ namespace / グループを使うノード間で枠を共有します。サーバー時刻と Lua の一括判定、
  cluster hash slot を揃えたキー、ランダムな一意 lease を使います。ノード停止後は lease 期限で枠が復旧します。
  起動時に Redis へ接続できなければ起動を失敗させ、実行中に障害があれば上限を迂回せず延期します。
  コマンドのタイムアウトは予約が成立したか不明な場合があるため、枠を保守的に消費する可能性があります。
  Redis の evict / データ消失 / 設定不一致は保証範囲外です。専用容量と適切な persistence / noeviction を設定してください。

### 監視と確認

`mx_shaping_message_admitted_total`、`mx_shaping_connection_admitted_total`、`mx_shaping_deferred_total`、
`mx_shaping_backend_error_total`、`mx_shaping_mx_cache_hit_total`、`mx_shaping_mx_lookup_error_total` と
worker 側の `worker_shaping_deferred_total` / `worker_shaping_domain_deferred_total` を確認します。
グループ・宛先をメトリクス名に含めず、カーディナリティの増加を避けます。
延期が増えた場合は rate/burst・接続上限・Redis 到達性・MX の変化と provider マッピングを確認してください。

実 Redis を使う結合テストは、ローカルの `redis-server` を一時ポート・永続化なしで起動します。

```bash
MTA_TEST_REDIS_SERVER=/path/to/redis-server go test -race -tags=integration ./internal/shaping ./internal/worker -run '^TestRedis' -v
```

独立した2クライアント（ノード相当）で合計同時数・rate/burst・別グループの進行・期限切れ lease・障害時の延期を確認します。
通常テストでは時計を制御し、TTL後の MX変更・暗黙MX・DNS失敗、worker の非待機・キャンセル・延期時の試行回数維持を確認します。
