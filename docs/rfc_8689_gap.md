# RFC 8689 REQUIRETLS Gap Note

`kuroshio-mta` は、認証必須の Submission から受け付けたメッセージについて RFC 8689 の REQUIRETLS 要求を保持し、配送を fail closed にします。

## 対応範囲

- 認証必須 Submission かつ、設定された全配送経路が SMTP の `mx` / `relay` の場合だけ `REQUIRETLS` を広告します
- `MAIL FROM` の `REQUIRETLS` をキュー JSON に保存し、再送後も維持します
- next hop に STARTTLS、検証済み TLS、TLS 後の `REQUIRETLS` 広告を要求します
- 条件を満たした next hop へ `MAIL FROM ... REQUIRETLS` を送ります
- TLS または拡張が利用できない場合は平文配送せず、`5.7.30` の恒久失敗として DSN を生成します
- `local_spool` と LMTP は、保証境界を定義できないため REQUIRETLS メッセージを拒否します

## 非対応・判断保留

- 通常の受信 SMTP では段階導入の対象外として広告・受理しません
- `TLS-Required: No` は DANE / MTA-STS を弱める可能性があるため解釈しません
- REQUIRETLS を指定した null reverse-path の通知を新たに生成せず、既存の DSN ループ防止を維持します
- 将来 LMTP やローカル配送を対応扱いにするには、最終配送点と保護境界を別途定義する必要があります
