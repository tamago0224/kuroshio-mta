# 正規化方針

この文書は、`kuroshio-mta` が受信、内部保持、通知生成の各段階で
どのような正規化を行うかをまとめたものです。

Postfix の調査結果は参考にしていますが、この文書自体は
「本 MTA が何を正規化し、何を正規化しないか」を示す設計ドキュメントとして管理します。

## 基本方針

`kuroshio-mta` の正規化は、次の原則に沿って行います。

- envelope は内部で扱いやすい最小限の標準形へ寄せる
- 受信した本文やリモート由来ヘッダは、必要最小限を除いて安易に書き換えない
- 自分で生成するヘッダや通知は、単純で安全な形式へ正規化する
- 相互運用より安全性を優先すべき箇所では、正規化ではなく reject を選ぶ

## 1. Envelope アドレスの正規化

受信側 SMTP セッションでは、`MAIL FROM` と `RCPT TO` を内部表現へ変換する前に
最小限の正規化を行います。

現在の実装で行っている挙動は次の通りです。

- 前後の空白は除去します
- `<addr@example.com>` 形式の山括弧は外します
- 内部保持する mailbox path 全体を小文字化します
- 空白、タブ、CR、LF を含む path は reject します
- `@` を含まない mailbox は reject します
- `MAIL FROM:<>` は null reverse-path として受理し、内部では空文字として保持します
- `RCPT TO:<>` は空 recipient として reject します
- `RCPT TO:<postmaster>` は `postmaster@<hostname>` に補完します
- UTF-8 アドレスと `SMTPUTF8` パラメータは reject します

この段階では、Postfix のような canonical mapping、masquerade、virtual alias 展開は行いません。
`kuroshio-mta` の envelope 正規化は、あくまで内部表現の単純化と安全性確保が目的です。

## 2. SMTP パラメータの正規化

`MAIL FROM` / `RCPT TO` に付く拡張パラメータは、受理するものを明示的に絞っています。

- `MAIL FROM` では `SIZE=<non-negative integer>` と `BODY=7BIT|8BITMIME` だけを受理します
- `SMTPUTF8` は reject します
- 未対応の `MAIL FROM` パラメータは reject します
- `key=value` になっていない不正な `MAIL FROM` パラメータも reject します
- `RCPT TO` パラメータは現状すべて reject します

この方針により、受信時に曖昧な拡張状態を内部へ持ち込まず、
今の実装が理解できる範囲だけを通します。

## 3. DATA 入力の正規化

SMTP `DATA` で受け取る本文は、キュー投入前に SMTP として必要な最低限の整形だけ行います。

- dot-stuffing は解除します
- 入力行は CRLF 区切りで内部バッファへ再構成します
- 1000 octet を超える入力行は reject します
- メッセージ全体が `max_message_bytes` を超える場合は reject します

ここでは、受信したメッセージの本文や既存ヘッダを大きく書き換えることはしません。
`cleanup` 的な広範なヘッダ再構成は、現時点では実装していません。

## 4. 受信トレースヘッダの正規化

キュー投入時には、受信トレースとして `Received:` ヘッダを先頭へ追加します。

このヘッダ生成では、注入を避けるために値を正規化します。

- hostname、HELO、remote address、message id は単一行へ正規化します
- CR/LF は除去します
- 制御文字や `(` `)` `;` `\\` `:` などは `_` に置き換えます
- 長すぎる token は 255 文字までに切り詰めます
- remote address が IP として解釈できる場合は IP 文字列を優先します
- protocol marker は `SMTP` / `ESMTP` / `SMTPS` / `ESMTPS` を使い分けます

この処理は、リモートが渡してきた値をそのまま trace header に埋め込まないための正規化です。

## 5. 自動生成メッセージの正規化

`kuroshio-mta` が自分で生成する DSN や DMARC report は、
受信メールとは別に、送出しやすい単純な形式へ正規化して組み立てます。

### 5.1 DSN

配送失敗・遅延通知では次を行います。

- 元メッセージの sender は trim して検証します
- null reverse-path または空 sender のメッセージに対しては DSN を生成しません
- `Auto-Submitted` が `no` 以外のメッセージに対しては DSN を生成しません
- 失敗 recipient は trim して小文字化します
- 生成 DSN の envelope sender は常に null reverse-path にします
- `From: MAILER-DAEMON@<hostname>` を付与します
- multipart/report の整った MIME 形式で生成します

ここでの loop 防止は、受信メール全体を rewrite するのではなく、
通知生成条件と DSN の envelope を制限する形で実装しています。

### 5.2 DMARC report

DMARC aggregate / failure report の生成時は次を行います。

- 本文中の改行は一度 `\n` に正規化してから CRLF に戻します
- `From` `To` `Subject` `Date` `Message-ID` を自前で付与します
- 生成ヘッダ値から CR/LF を除去します

自動生成メッセージでは、入力をそのまま透過するよりも、
安全で予測可能な形式へ寄せることを優先します。

## 6. 認証済み Submission の正規化ルール

Submission では、認証済みユーザーと envelope sender の整合性を追加で確認します。

- 認証ユーザーのドメインは小文字化して比較します
- `MAIL FROM` のドメインも小文字化して比較します
- `submission_enforce_sender_identity` が有効な場合は、両者が一致しない sender を reject します

これは sender rewrite ではなく、Submission 専用の受理条件として扱います。

## 7. 本 MTA が現時点で行わない正規化

次のような Postfix 的な広範囲の書き換えは、現時点の `kuroshio-mta` では行いません。

- canonical mapping
- masquerading
- virtual alias 展開
- remote header の包括的 rewrite
- 受信メールからの `Bcc` / `Return-Path` の一律除去
- 配送先解決のための address class rewrite

このプロジェクトでは、まず envelope と自動生成メッセージの正規化を明確にし、
リモート由来の本文やヘッダはできるだけ意味を保ったまま扱う方針を取ります。

## 8. 今後の拡張余地

将来的に追加を検討しうる領域はありますが、
導入時は「互換性のための便宜」と「意味のない改変」を混同しないことを重視します。

候補:

- 受信時のヘッダ整形ポリシーの明文化
- 自動補完するヘッダの範囲整理
- alias / rewrite を導入する場合の適用段階分離
- 配送前にだけ適用する sender rewrite の設計

新しい正規化を入れるときは、受信時、内部保持時、配送時のどこで適用するのかを分けて管理します。
