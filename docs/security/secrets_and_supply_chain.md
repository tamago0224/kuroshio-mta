# Secrets and Software Supply Chain Security

## Secrets Handling

- `MTA_SUBMISSION_USERS` は平文環境変数の代わりに `MTA_SUBMISSION_USERS_FILE` を利用可能
- `_FILE` 方式では、指定ファイルの内容を読み込んでシークレット値として利用
- ログ出力ではメールアドレス等のPII（Personally Identifiable Information）をマスキング

## CI Vulnerability Scanning

- GitHub Actions `security.yml` で `govulncheck` を常時実行
- 脆弱性検知時はジョブ失敗となり、マージ前にブロック可能

## SBOM Generation

- `scripts/security/generate_sbom.sh` で Go module graph ベースの SBOM を生成
- CI では `artifacts/sbom/go-modules.sbom.json` を成果物として保存

## Signature (cosign example)

本リポジトリはコンテナイメージをビルドしないため、まず SBOM ファイル署名を対象にする。

```bash
cosign sign-blob --yes \
  --output-signature go-modules.sbom.sig \
  artifacts/sbom/go-modules.sbom.json
```

検証:

```bash
cosign verify-blob \
  --signature go-modules.sbom.sig \
  artifacts/sbom/go-modules.sbom.json
```

## IAM (Identity and Access Management) Least Privilege

- CI token permissions は `contents:read` を基本とする
- デプロイ時シークレットアクセスは実行環境の最小権限ロールに限定
- 鍵管理は KMS（Key Management Service）連携を前提に段階移行する
