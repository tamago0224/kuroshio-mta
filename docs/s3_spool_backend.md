# S3 Spool Backend

`kuroshio-mta` では `delivery_mode: local_spool` を使うと、SMTP 外へ配送せずに `.eml` を保存できます。
この保存先は `spool_backend` で切り替えられ、現時点では `local` と `s3` を使えます。

`spool_backend: s3` は AWS S3 固有ではなく、S3-compatible object storage を想定しています。
MinIO、Ceph RGW、SeaweedFS、Garage なども対象にできます。

## 使いどころ

- 単一ノードのローカル保存で十分な場合: `spool_backend: local`
- ローカルファイルシステム依存を減らしたい場合: `spool_backend: s3`
- `.eml` 本文をオブジェクトとして保存したい場合: `spool_backend: s3`

## 最小設定例

```yaml
delivery_mode: local_spool
spool_backend: s3

spool_s3_bucket: mail-spool
spool_s3_prefix: outbound
spool_s3_endpoint: http://localhost:9000
spool_s3_region: us-east-1
spool_s3_access_key: minioadmin
spool_s3_secret_key: minioadmin
spool_s3_force_path_style: true
spool_s3_use_tls: false
```

この設定では、保存先はローカルディスクではなく object storage になります。

## MinIO を使う例

ローカル検証では MinIO のような S3-compatible storage と相性が良いです。

```yaml
delivery_mode: local_spool
spool_backend: s3
spool_s3_bucket: mail-spool
spool_s3_prefix: dev
spool_s3_endpoint: http://minio:9000
spool_s3_region: us-east-1
spool_s3_access_key: minioadmin
spool_s3_secret_key: minioadmin
spool_s3_force_path_style: true
spool_s3_use_tls: false
```

補足:

- `spool_s3_force_path_style: true` は MinIO などで使うことが多い設定です
- `spool_s3_endpoint` は `http://...` / `https://...` 付きでも、ホスト名だけでも指定できます
- スキームを省略した場合は `spool_s3_use_tls` に応じて `http` / `https` を補います

## 保存される object key

保存キーは次のような形です。

```text
<prefix>/<message-id>_<sanitized-recipient>.eml
```

例:

```text
outbound/m123_user_example.net.eml
```

`spool_s3_prefix` が空なら、ファイル名だけを object key として使います。

## 必須項目

`spool_backend: s3` のときは、少なくとも次が必要です。

- `spool_s3_bucket`

実運用では通常、次も設定します。

- `spool_s3_endpoint`
- `spool_s3_region`
- `spool_s3_access_key`
- `spool_s3_secret_key`

## いまの制約

- 現在の実装は `PutObject` による保存までです
- object の一覧取得や再読込はまだ実装していません
- provider 固有機能には依存していません

## 関連ドキュメント

- 全体設定: [Configuration](/configuration)
- 起動手順: [Getting Started](/getting-started)
- 今後の設計背景: [Spec](/spec)
