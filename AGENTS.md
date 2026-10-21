# AGENTS.md

## 目的
このリポジトリは GitHub Flow を前提に運用します。  
エージェントは、小さく安全でレビューしやすい変更を行い、ブランチ運用と Pull Request を基本に作業してください。

---

## 作業方針

### ブランチ運用
- 明示的な指示がない限り、`main` へ直接コミットしないこと
- コード変更前に、作業用ブランチを作成または切り替えること
- ブランチ名は以下を推奨します
  - `feature/<概要>`
  - `fix/<概要>`
  - `chore/<概要>`
  - `docs/<概要>`

例:
- `feature/add-login-api`
- `fix/null-pointer-in-user-service`
- `chore/update-dependencies`

### main の同期
実装開始前に、可能であれば最新の `main` を取り込んでください。

推奨手順:
1. `git fetch --prune origin`
2. `git switch main`
3. `git pull --rebase origin main`
4. `git switch -c <作業ブランチ>`

すでに作業ブランチにいる場合は、必要に応じて `main` を rebase してください。

### コミット
- コミットは小さく、目的が明確な単位で行うこと
- 無関係な変更を1つのコミットに混ぜないこと
- コミットメッセージは簡潔かつ内容が分かるものにすること

推奨形式:
- `feat: ログインAPIを追加`
- `fix: ユーザー取得時のnull参照を修正`
- `docs: セットアップ手順を更新`
- `refactor: バリデーション処理を整理`
- `test: 認証ミドルウェアのテストを追加`
- `chore: ツール設定を更新`

### Pull Request
作業完了時は以下を基本とします。
- 現在のブランチを `origin` に push する
- `main` 向けの Pull Request を作成する
- 必要に応じて `gh pr create --fill` を使う
- PR は小さくレビューしやすい状態に保つ

PR を作成・準備する際は、以下を整理してください。
- 変更概要
- 実施した確認内容
- 残課題や懸念点

---

## 実装時の振る舞い

### 基本方針
- 編集前に、対象コードと関連箇所を把握すること
- 大規模な書き換えよりも、必要最小限の変更を優先すること
- 明示的な依頼がない限り、既存の設計・構成・命名規則を尊重すること
- 不要な依存追加は避けること
- 無関係なファイルは変更しないこと
- 曖昧な場合は、安全側の解釈を選び、その前提を明示すること

### 安全性
- 破壊的な操作は、明確に必要な場合を除いて避けること
- `force-push`、`reset --hard`、履歴改変、大量削除は慎重に扱うこと
- ユーザーの既存作業を安易に上書きしないこと
- 可能な限り、可逆な操作を優先すること

### 確認・検証
作業完了前に、変更内容に対して最小限かつ意味のある確認を行うこと。

例:
- フォーマット実行
- 変更範囲に関連するテスト実行
- 必要に応じたビルド
- lint / vet の実行

Go プロジェクトでは、必要に応じて以下を優先してください。
- `gofmt -w ...`
- `go test ./...` または対象を絞ったテスト
- `go build ./...`
- `go vet ./...`

実際に確認していない内容を「動作確認済み」と表現しないこと。

---

## GitHub CLI の利用
GitHub Flow に必要な範囲で、GitHub CLI を利用してよいものとします。

許可される例:
- issue の確認・作成
- PR の確認・作成
- PR の状態確認
- project 情報の確認

現在のタスクに関係する、必要最小限の操作を優先してください。

---

## 推奨ワークフロー

### 新規実装
1. リポジトリ構成と関連ファイルを確認する
2. `main` 以外の作業ブランチで作業する
3. 最小限の変更で実装する
4. 必要な確認を行う
5. 分かりやすいメッセージでコミットする
6. ブランチを origin に push する
7. 必要に応じて PR を作成する

### バグ修正
1. 不具合箇所を再現または調査する
2. 最小限かつ安全な修正を行う
3. 可能であればテストを追加・更新する
4. 修正内容を確認する
5. コミットし、必要に応じて PR を準備する

### ドキュメント・雑務
1. 変更範囲を限定する
2. 必要のないリファクタや設計変更を混ぜない
3. 分かりやすくコミットする
4. 必要に応じて PR を作成する

---

## コミュニケーション
- 報告は簡潔かつ事実ベースで行うこと
- 「何を変えたか」「どう確認したか」「残る懸念は何か」を明確にすること
- 詰まりや懸念があれば早めに共有すること
- 未確認の内容を完了済みのように扱わないこと

---

## リポジトリ固有ルール
- README、CONTRIBUTING、設計資料などに追加ルールがある場合はそれを優先すること
- 指示の優先順位は以下とする
  1. ユーザーからの直接指示
  2. リポジトリ固有ドキュメント
  3. この AGENTS.md

---

## 禁止・非推奨事項
- `main` への直接コミット
- 無関係な大規模リファクタ
- 意図説明のない依存更新
- レビューしづらい巨大コミット
- 明示的必要性のない破壊的 Git 操作
- 実行していないテストを「成功した」と表現すること

---

## プロジェクト概要と調査の入口

`kuroshio-mta` は Go で実装した MTA です。SMTP / Submission の受信、永続キュー、配送・再送、メール認証、管理 API、可観測性を扱います。SMTP 処理は外部 SMTP ライブラリに依存していません。

- Go の module path は `github.com/tamago0224/kuroshio-mta`。import はこのパスに合わせること
- Go バージョンは `go.mod` と `.github/workflows/ci.yml` を参照すること。現在は `go 1.26.10`、CI と Docker のビルド環境も `1.26.10`
- Node.js は VitePress ドキュメント用。docs CI は Node.js 22 と `npm ci` を使用する
- 最初に `README.md`、`docs/getting-started.md`、`docs/configuration.md` と変更対象の実装・テストを読むこと
- `docs/spec.md` は目標アーキテクチャを含む。記載された構成や性能目標を、実装済み・測定済みとみなさないこと
- プロトコル変更では `docs/rfc_*_gap.md`、設計判断では `docs/architecture/`、運用変更では `docs/runbooks/` を確認すること

## コード配置

| 場所 | 役割 |
| --- | --- |
| `cmd/kuroshio/main.go` | 設定の読み込み、依存の組み立て、各サーバー・ワーカーの起動と停止 |
| `cmd/smtpload/` | SMTP 負荷生成 CLI |
| `internal/config/` | YAML・環境変数からの設定読み込みと検証 |
| `internal/smtp/` | SMTP / Submission セッション、STARTTLS、SMTP AUTH、conformance テスト |
| `internal/ingress/` | DNSBL、レート制限、Redis を使う制限ストア |
| `internal/mailauth/`、`internal/dkim/` | 受信時の SPF / DKIM / DMARC / ARC 評価と送信署名 |
| `internal/model/`、`internal/queue/` | メッセージ表現、共通 Backend、ローカル・Kafka キュー |
| `internal/worker/` | 配送の実行、再送、ドメイン単位のスロットリング |
| `internal/delivery/`、`internal/router/` | SMTP 配送、MTA-STS / DANE、S3 spool、MX 解決 |
| `internal/bounce/`、`internal/reputation/`、`internal/retention/` | DSN、suppression、レピュテーション、保存期限管理 |
| `internal/admin/`、`internal/userauth/` | 管理 API と管理認証、Submission ユーザー認証 |
| `internal/logging/`、`internal/observability/` | slog、PII マスキング、メトリクス、OpenTelemetry |
| `integration/`、`test/integration/` | integration タグ付きテストと Docker による DNS 検証環境 |
| `examples/tutorials/` | チュートリアルの Compose 環境と設定例 |
| `scripts/`、`deploy/monitoring/` | 管理・負荷・復旧・セキュリティの補助スクリプトと監視ルール |
| `docs/`、`docs/.vitepress/config.mts` | ドキュメント本文とサイト構成 |

## 変更時の注意

- 設定項目を変更するときは `internal/config/config.go` とそのテスト、`config.example.yaml`、`docs/configuration.md` の整合を確認すること。関連するチュートリアルの設定例も確認する
- 設定ファイルの選択順序は `-config`、`MTA_CONFIG_FILE`、カレントディレクトリの `config.yaml` / `config.yml`。値の優先順位や既定値は読み込み実装で確認すること
- キューの変更では `internal/queue/backend.go` の契約とローカル・Kafka 両方への影響を確認すること。メッセージの JSON 表現や状態遷移を変える際は、既存の保存データ、再送、管理 API、復旧処理との互換性を考慮する
- SMTP の入力処理、envelope、ヘッダ、本文、DSN を変更する前に `docs/architecture/normalization_policy.md` を読むこと。CRLF、dot-stuffing、サイズ制限、null reverse-path、通知ループ防止への影響を確認する
- TLS・認証・配送ポリシーの変更では、拒否条件や失敗時の挙動を確認すること。既存の制約を意図せず緩めない
- ログは既存の `log/slog` と `internal/logging/` の流儀に合わせること。メールアドレス・IP のマスキングを維持し、本文、認証情報、秘密鍵、トークンをログやコミットに含めない
- シークレットの扱いは `docs/security/secrets_and_supply_chain.md` を参照すること
- サイトにページを追加・移動した場合は `docs/.vitepress/config.mts` のナビゲーションと関連リンクも確認すること。文書内のリンクはリポジトリ相対パスを使用する
- `node_modules/`、`docs/.vitepress/cache/`、`docs/.vitepress/.temp/`、`docs/.vitepress/dist/` は生成物。直接編集・コミットしないこと。`var/` 内の実行データもコミットしない

## 開発・確認コマンド

以下はリポジトリルートで実行します。変更範囲に必要な確認を選び、未実施の確認は明記してください。

| 目的 | コマンド・前提 |
| --- | --- |
| ローカル起動 | `cp config.example.yaml config.yaml` の後、`go run ./cmd/kuroshio -config ./config.yaml`。既存の設定ファイルを上書きしないこと |
| Go の整形 | `gofmt -w <変更したGoファイル>` |
| 対象パッケージの確認 | `go test ./internal/<対象パッケージ>` |
| Go 全体の確認 | `go vet ./...`、`go test ./...`、`go test -race ./...` |
| ビルド | `go build ./...` |
| SMTP conformance | `go test ./internal/smtp -run '^TestSMTPConformance$' -v` |
| DNS 結合テスト | Docker が必要。`./scripts/integration/run_dns_env_tests.sh` |
| docs 依存の準備 | `npm ci` |
| docs 開発サーバー | `npm run docs:dev` |
| docs ビルド | `npm run docs:build` |

- `.github/workflows/ci.yml` は vet、通常テスト、race テスト、SMTP conformance を実行する
- DNS 結合テストは通常の `go test ./...` とは別。上記スクリプトが DNS / policy サービスを起動して `go test -tags=integration ./integration/...` を実行する。終了時に専用 Compose 環境を `down -v` するため、共有の検証データを置かないこと
- docs の本文・サイト構成を変更した場合は docs ビルドでリンク等を確認する。AGENTS.md だけの変更では、参照パス・コマンド・差分の確認を基本とする
- `.github/workflows/security.yml` は `govulncheck ./...` と SBOM 生成を実行する。依存変更時はこれらへの影響も確認する
- PR 本文は `.github/pull_request_template.md` に従うこと。Validation / TDD のチェック欄は実施した項目だけをチェックし、文書変更などで該当しない場合は理由を記載する
