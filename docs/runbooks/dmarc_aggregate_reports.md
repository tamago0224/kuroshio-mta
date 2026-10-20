# DMARC aggregate reports

受信SMTPは RFC 9989 shadow 評価の結果に `rua` がある場合、RFC 9990 aggregate report 用の観測値をローカルSQLiteへ保存します。配送判定は引き続き既存のポリシー評価に従います。外部の受信先へは、RFC 9990 のDNS認可を通った `mailto:` URI のみ送信します。

## 有効化と保存先

ドメイン所有者が `_dmarc` レコードの `rua=mailto:...` を公開すると、そのレポート先が有効になります。ポリシードメインとレポート先の組織ドメインが異なる場合は、次の名前に `v=DMARC1` で始まるTXT認可レコードが必要です。

```text
<policy-domain>._report._dmarc.<report-destination-domain>
```

DNS応答がない、無効な認可レコードである、またはDNSが一時失敗した場合、その宛先にはレポートを作りません。一時DNS失敗はログに記録し、次のメール観測時に再評価します。

SQLite DB は `queue_dir/dmarc-reports.db` に作成されます。`queue_dir` は永続ボリュームに置き、実行ユーザーだけが読めるアクセス権を維持してください。DBにはIPアドレス、ドメイン、DKIM/SPF結果、認可済みRUAメールアドレスが含まれます。メール本文、個々の受信者アドレス、メッセージIDは保存しません。

## 生成と再試行

- 集計期間はUTC日次です。完了した期間を1分ごとに確認し、RFC 9990 XMLをgzipしてメールキューへ投入します。
- XMLには `urn:ietf:params:xml:ns:dmarc-2.0` namespace、期間、policy/discovery情報、送信元IP、認証結果を含めます。
- 同じタプルを期間内で件数集約し、1レポート1,000行を超える場合は分割します。XMLが5 MiBを超える場合も分割します。1タプル単体で上限を超える場合は保持してエラーを記録します。
- キュー投入に失敗した期間はDBに残り、次の周期に再試行します。report IDとキューIDは決定的です。ローカルキューでは同じIDの重複投入を抑止し、Kafkaでは少なくとも1回配送となるため同一report IDの重複を受信側で許容してください。
- SQLiteは単一プロセス用です。同一 `queue_dir` を複数レプリカで共有しないでください。
- 生成メールはnull reverse-pathと `Auto-Submitted` / 内部識別ヘッダーを使います。通常のDKIM設定または一致するDKIM profileがある場合は署名されます。識別ヘッダー付きレポートを再受信した場合は集計対象から除外します。

## 運用と制限

起動後、`queue_dir/dmarc-reports.db` の存在・書き込み可能性と、通常の配送キューに集計レポートが投入されることを確認します。集計保存、DNS認可、XML生成、キュー投入の失敗は `dmarc_report` または `smtp` コンポーネントのログに現れます。`dmarc_aggregate_observations_total`、`dmarc_aggregate_observation_errors_total`、`dmarc_aggregate_destination_rejected_total`、`dmarc_aggregate_destination_errors_total`、`dmarc_aggregate_flush_runs_total`、`dmarc_aggregate_flush_errors_total` カウンターで保存・認可・flush の進行を確認します。失敗が続く場合は、DBディスク容量、DNS解決、キューbackend、宛先認可TXTを確認してください。保留中の期間は手動削除せず、原因を直して再試行してください。

DBの追加タプル数は全期間合計100,000件に制限されています。上限に達した場合は新規観測の保存が失敗します。自動保持期限・管理APIはまだありません。DB容量とエラーカウンターを監視し、長期間キュー障害が続いた場合は復旧前にDBをバックアップしてください。

旧実装はメール1通ごとに誤ったプレーンテキスト「aggregate report」を生成していました。新実装はその生成を停止します。切替前にすでに通常キューへ入った通知は通常メールとして残るため、必要に応じてキューを運用手順に沿って処理してください。過去の単通通知を新しい日次XMLへ変換することはできません。RUF failure report は別機能として従来の形式で残り、RFC 9991対応の完了を意味しません。

仕様: [RFC 9990](https://www.rfc-editor.org/rfc/rfc9990.html)、[DMARC RFC 9989/7489 gap](../rfc_7489_gap.md)。
