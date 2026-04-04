# Rate Limit 設定

Rate Limit は `config.yaml` の `rate_limit_rules` を主として管理し、必要なら `MTA_RATE_LIMIT_RULES` で上書きします。

## 対応する設定項目

| YAML property | 環境変数 | default | 説明 |
| --- | --- | --- | --- |
| `ingress_rate_limit_per_minute` | `MTA_INGRESS_RATE_LIMIT_PER_MINUTE` | `100` | 単純な受信レート制限のベース値です |
| `rate_limit_rules` | `MTA_RATE_LIMIT_RULES` | unset | イベント別の詳細ルールです |

## フォーマット

`rate_limit_rules` は `event:key:limit:window;...` 形式で指定します。

- `event`: `connect` / `helo` / `mailfrom`
- `key`: `ip` / `helo` / `mailfrom` / `ip+helo` / `ip+mailfrom`
- `limit`: 許可回数
- `window`: 期間（例: `10s`, `1m`, `5m`, `1h`）

## YAML 例

```yaml
ingress_rate_limit_per_minute: 100
rate_limit_rules: "connect:ip:100:1m;helo:ip+helo:20:1m;mailfrom:ip+mailfrom:30:5m"
```

ルールを分けて見やすくしたい場合は、コメントを添えて管理すると運用しやすくなります。

```yaml
ingress_rate_limit_per_minute: 100

# 1分間に接続100回まで（IP単位）
# 1分間に HELO ごとに 20回まで（IP+HELO 単位）
# 5分間に MAIL FROM ごとに 30回まで（IP+MAIL FROM 単位）
rate_limit_rules: "connect:ip:100:1m;helo:ip+helo:20:1m;mailfrom:ip+mailfrom:30:5m"
```

## 環境変数で上書きする例

```bash
MTA_RATE_LIMIT_RULES="connect:ip:100:1m;helo:ip+helo:20:1m;mailfrom:ip+mailfrom:30:5m" \
go run ./cmd/kuroshio
```

## 運用メモ

- まずは YAML に基準値を書き、緊急時の一時上書きだけ環境変数に寄せると差分管理しやすくなります
- `ingress_rate_limit_per_minute` は全体の基本制御、`rate_limit_rules` はイベント単位の詳細制御という役割分担で使うと整理しやすいです
- 詳細な全体設定一覧は [configuration.md](/home/tamago/ghq/github.com/tamago/kuroshio-mta/docs/configuration.md) を参照してください
