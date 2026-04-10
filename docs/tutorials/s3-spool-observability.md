# S3 Spool Backend を観測する

`delivery_mode: local_spool` と `spool_backend: s3` を組み合わせて、受信したメールが S3-compatible object storage に保存される流れを確認する tutorial です。

この tutorial では `MinIO` を使い、SMTP で 1 通投入してから

- `kuroshio-mta` の metrics
- MTA のログ
- MinIO 上の object

を見て動作確認します。

## 前提

- Docker と `docker compose` が使える
- ローカルで `:2525`、`:9090`、`:9000`、`:9001` を使える

使う compose 一式は [examples/tutorials/s3-spool-observability](https://github.com/tamago0224/kuroshio-mta/tree/main/examples/tutorials/s3-spool-observability) にあります。

## 起動するもの

- `kuroshio`: tutorial 用設定で起動する `kuroshio-mta`
- `smtp-client`: SMTP セッション投入用
- `minio`: S3-compatible object storage
- `create-bucket`: tutorial 用 bucket を初期化する one-shot service

## 1. compose を起動する

```bash
docker compose -f examples/tutorials/s3-spool-observability/compose.yaml up --build -d
```

この tutorial では次の設定を使います。

- `delivery_mode: local_spool`
- `spool_backend: s3`
- `spool_s3_bucket: mail-spool`
- `spool_s3_prefix: tutorial`
- `spool_s3_endpoint: http://minio:9000`

## 2. SMTP で 1 通投入する

```bash
docker compose -f examples/tutorials/s3-spool-observability/compose.yaml exec smtp-client sh -lc '
cat <<EOF | nc kuroshio 2525
EHLO tutorial.local
MAIL FROM:<sender@example.com>
RCPT TO:<recipient@example.net>
DATA
Subject: S3 spool tutorial

hello to object storage
.
QUIT
EOF
'
```

## 3. metrics を確認する

```bash
curl http://127.0.0.1:9090/metrics | head
```

まずは observability endpoint が生きていることを確認します。

## 4. MinIO に object が入ったことを確認する

MinIO の Web UI は `http://127.0.0.1:9001` です。

- username: `minioadmin`
- password: `minioadmin`

CLI で見たい場合は次でも確認できます。

```bash
docker compose -f examples/tutorials/s3-spool-observability/compose.yaml exec minio sh -lc '
find /data -maxdepth 5 -type f | sort
'
```

`mail-spool/tutorial/` 配下に `.eml` object が作られていれば成功です。

## 5. ログを見る

```bash
docker compose -f examples/tutorials/s3-spool-observability/compose.yaml logs kuroshio
```

この tutorial では tracing stack までは立てていませんが、

- metrics: `:9090/metrics`
- logs: `docker compose logs`
- 保存結果: MinIO 上の object

の 3 点で backend 切り替え時の挙動を確認できます。

## 6. 後片付け

```bash
docker compose -f examples/tutorials/s3-spool-observability/compose.yaml down
rm -rf examples/tutorials/s3-spool-observability/var
```

## 関連

- [S3 Spool Backend](/s3_spool_backend)
- [Observability](/observability)
- [Tutorials Home](/tutorials/)
