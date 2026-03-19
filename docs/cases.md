# テストケース一覧

## アイドルタイムアウトの種類と違い

このプロジェクトには **2 種類のアイドルタイムアウト** が存在する。混同しないこと。

### ALB アイドルタイムアウト

| 項目 | 内容 |
|---|---|
| 設定箇所 | `terraform/terraform.tfvars` の `alb_idle_timeout`（秒） |
| 適用範囲 | **AWS 環境のみ**。ローカルには存在しない |
| 動作 | クライアント ↔ ALB 間、または ALB ↔ EC2 間のどちらかで、TCP レイヤーのデータが指定秒間流れなければ ALB が接続を強制切断する |
| 切断の見え方 | クライアントに `close 1006 (abnormal closure)` — ALB は WebSocket クローズハンドシェイクをせず TCP を切る |
| リセット条件 | クライアントが **ping フレームを送る**、または **データメッセージを送る**と、そのタイミングで ALB のカウンタがリセットされる。サーバーからの pong もデータとして扱われリセットされる |

### サーバー実装のアイドルタイムアウト

| 項目 | 内容 |
|---|---|
| 設定箇所 | `ws-server` の `-idle-timeout` フラグ（`0` で無効） |
| 適用範囲 | **ローカル・AWS 両方**で動作する |
| 動作 | `conn.SetReadDeadline()` により、指定秒間サーバーに何も届かなければサーバー側から接続を閉じる |
| 切断の見え方 | クライアントに `read error: ...`（サーバーが WebSocket クローズフレームを送ってから切断）|
| リセット条件 | サーバーが **データメッセージ**または **ping フレーム**を受信するたびに deadline をリセットする |

### AWS での両者の関係

AWS 環境では両方が同時に動作しうる。どちらの期限が先に来るかで挙動が変わる。

| 設定 | 先に切れるのは |
|---|---|
| サーバー timeout < ALB timeout | サーバーが先に切断（`read error` + サーバー側 close） |
| サーバー timeout > ALB timeout | ALB が先に切断（`close 1006`） |
| サーバー timeout = 0（無効） | ALB のみが切断源になる |

> **推奨**: AWS での検証時はサーバー側を `-idle-timeout 0`（無効）にしておくと、ALB の挙動だけを純粋に観察できる。

---

## ローカル

ALB は介在しないため、**サーバー実装のアイドルタイムアウトのみ**が動作する。

### 前準備

```bash
make build
```

---

### L-1: サーバー idle timeout でアイドル接続が切断される

**何を確認するか**: サーバーの `-idle-timeout` が機能し、アイドル接続を閉じること。

- ALB が無通信接続を切断するまでの時間: -（ローカルのため）
- サーバーが無通信接続を閉じるまでの時間: 10s
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: no

**ターミナル 1 (サーバー)**
```bash
./server/ws-server -idle-timeout 10s
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws
```

**期待結果 (サーバー)**
- 10 秒後に read deadline 超過を検知して接続を閉じる
- `disconnected` が出る

**期待結果 (クライアント)**
- `read error: ...` が出て終了する

---

### L-2: ping でサーバー idle timeout をリセットし接続を維持する

**何を確認するか**: クライアントの ping がサーバー側の read deadline をリセットすること。

- ALB が無通信接続を切断するまでの時間: -（ローカルのため）
- サーバーが無通信接続を閉じるまでの時間: 10s
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: yes
- ping の送信間隔: 5s（サーバー timeout より短い）
- 指定時間後にデータ送信して終了: no

**ターミナル 1 (サーバー)**
```bash
./server/ws-server -idle-timeout 10s
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws -ping -ping-interval 5s
```

**期待結果 (サーバー)**
- `received ping, sending pong` が繰り返し出る
- ping handler が `resetDeadline()` を呼び出すため 10 秒の deadline が毎回延長され、切断されない

**期待結果 (クライアント)**
- 5 秒ごとに `[ping] sending at ...` が出る
- `[pong] received: ...` を受信し続ける
- 10 秒以上接続が維持される

---

### L-3: サーバーが pong を返さない (-no-pong)

**何を確認するか**: `-no-pong` でサーバーが pong を返さないこと。ただし ping 受信時の deadline リセットは行われるため、idle timeout はリセットされる。

- ALB が無通信接続を切断するまでの時間: -（ローカルのため）
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: no
- クライアントから定期 ping を送る: yes
- ping の送信間隔: 5s
- 指定時間後にデータ送信して終了: no

**ターミナル 1 (サーバー)**
```bash
./server/ws-server -no-pong
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws -ping -ping-interval 5s
```

**期待結果 (サーバー)**
- `received ping (pong suppressed)` が繰り返し出る
- 接続は切れない

**期待結果 (クライアント)**
- `[pong] received` が出ない
- 接続は切れない（pong 未受信はクライアント側の切断原因にはならない）

---

### L-4: -no-pong かつサーバー idle timeout の組み合わせ

**何を確認するか**: pong を返さなくても、ping がサーバーの read deadline をリセットするため接続が維持されること。

- ALB が無通信接続を切断するまでの時間: -（ローカルのため）
- サーバーが無通信接続を閉じるまでの時間: 10s
- サーバーが pong を返す: no
- クライアントから定期 ping を送る: yes
- ping の送信間隔: 5s（サーバー timeout より短い）
- 指定時間後にデータ送信して終了: no

**ターミナル 1 (サーバー)**
```bash
./server/ws-server -idle-timeout 10s -no-pong
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws -ping -ping-interval 5s
```

**期待結果 (サーバー)**
- `received ping (pong suppressed)` が繰り返し出る
- ping が read deadline をリセットするため 10 秒を超えても切断されない

**期待結果 (クライアント)**
- `[pong] received` は出ない
- 接続は維持される

---

### L-5: -send-after でデータ送信・応答確認後に終了する

**何を確認するか**: `-send-after` によりデータ送受信の一往復後に正常終了すること。

- ALB が無通信接続を切断するまでの時間: -（ローカルのため）
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: yes（3s 後）

**ターミナル 1 (サーバー)**
```bash
./server/ws-server
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws -send-after 3s
```

**期待結果 (サーバー)**
- メッセージを受信してエコーを返す
- クライアントからの close フレームを受けて `disconnected` が出る

**期待結果 (クライアント)**
- 3 秒後に `[send] sending data: ...` が出る
- `[recv] ...` でエコーを受信する
- `[send] response received, closing connection` と出力して正常終了する

---

### L-6: -send-after で応答が来ない場合にタイムアウト終了する

**何を確認するか**: サーバーが応答しない場合、クライアントが 10 秒後にタイムアウト終了すること。

- ALB が無通信接続を切断するまでの時間: -（ローカルのため）
- サーバーが無通信接続を閉じるまでの時間: なし（無効）、ただし途中で停止する
- サーバーが pong を返す: yes（ただし途中で停止する）
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: yes（1s 後、応答待ち 10s）

**ターミナル 1 (サーバー — 接続確認後に Ctrl-C)**
```bash
./server/ws-server
# クライアントの接続ログを確認後に Ctrl-C
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws -send-after 1s
```

**期待結果 (サーバー)**
- Ctrl-C で停止済み

**期待結果 (クライアント)**
- サーバー停止後にデータ送信を試みた場合: `read error: ...` が出て即時終了する
- サーバーが生きていてもエコーを返さない場合: 10 秒後に `[send] timeout waiting for response` が出て終了する

---

### L-7: ping 間隔がサーバー idle timeout より長い場合に切断される

**何を確認するか**: ping が有効でも間隔がサーバー timeout より長ければ timeout が先に来て切断されること。L-2 の逆。

- ALB が無通信接続を切断するまでの時間: -（ローカルのため）
- サーバーが無通信接続を閉じるまでの時間: 10s
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: yes
- ping の送信間隔: 15s（サーバー timeout より長い）
- 指定時間後にデータ送信して終了: no

**ターミナル 1 (サーバー)**
```bash
./server/ws-server -idle-timeout 10s
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws -ping -ping-interval 15s
```

**期待結果 (サーバー)**
- 10 秒後に read deadline 超過を検知して接続を閉じる
- `disconnected` が出る

**期待結果 (クライアント)**
- `read error: ...` が出て終了する
- `[ping] sending at ...` は出力されない（ping より先に切断される）

---

### L-8: send-after がサーバー idle timeout より長い場合、接続が先に切れる

**何を確認するか**: send-after の発火より前にサーバー timeout が来て接続が閉じられると、クライアントはデータを送らずに終了すること。

- ALB が無通信接続を切断するまでの時間: -（ローカルのため）
- サーバーが無通信接続を閉じるまでの時間: 5s
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: yes（10s 後、サーバー timeout より長い）

**ターミナル 1 (サーバー)**
```bash
./server/ws-server -idle-timeout 5s
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws -send-after 10s
```

**期待結果 (サーバー)**
- 5 秒後に read deadline 超過を検知して接続を閉じる
- `disconnected` が出る

**期待結果 (クライアント)**
- 5 秒後に `read error: ...` が出る
- send-after（10s 後）が来る前に `case <-done` が選ばれて終了する
- `[send] sending data:` は出力されない

---

### L-9: 単体テスト

```bash
make test
```

または個別に:

```bash
make test-server   # cd server && go test -v ./...
make test-client   # cd client && go test -v ./...
```

---

## AWS (ALB 経由)

**ALB アイドルタイムアウト**と**サーバー実装のアイドルタイムアウト**が両方動作しうる。
切断原因を ALB に絞りたい場合は、サーバーを `-idle-timeout 0`（デフォルト、無効）で起動すること。

事前に `make infra-up` と `make deploy` が完了していること。

```bash
export WS_URL=$(cd terraform && terraform output -raw websocket_url)
```

---

### A-1: ALB idle timeout でアイドル接続が切断される

**何を確認するか**: サーバー側 timeout を無効にした状態で、ALB だけが切断源になること。切断時の error code が `1006`（ALB による強制切断）であることを確認する。

- ALB が無通信接続を切断するまでの時間: 60s
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: no

```bash
# サーバーは -idle-timeout なし（デフォルト 0 = 無効）でデプロイ済みであること
make client-idle
# または
./client/ws-client -url "$WS_URL"
```

**期待結果 (サーバー)**
- ALB が TCP を切断するため、サーバー側では接続エラーが発生して `error: ...` または `disconnected` が出る

**期待結果 (クライアント)**
- `alb_idle_timeout` 秒後に `read error: websocket: close 1006 (abnormal closure)` が出て終了する
- `1006` = ALB が WebSocket クローズハンドシェイクをせず TCP を強制切断したことを示す

---

### A-2: ping で ALB idle timeout を回避し接続を維持する

**何を確認するか**: クライアントの ping フレームが ALB のカウンタをリセットし、`alb_idle_timeout` を超えても接続が維持されること。

- ALB が無通信接続を切断するまでの時間: 60s
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: yes
- ping の送信間隔: 50s（ALB timeout より短い）
- 指定時間後にデータ送信して終了: no

```bash
make client-ping PING_INTERVAL=50s
# または（alb_idle_timeout=60s の場合）
./client/ws-client -url "$WS_URL" -ping -ping-interval 50s
```

**期待結果 (サーバー)**
- `received ping, sending pong` が 50 秒ごとに繰り返し出る
- 接続が切れない

**期待結果 (クライアント)**
- 50 秒ごとに `[ping] sending at ...` と `[pong] received: ...` が交互に出続ける
- ALB idle timeout (60 秒) を超えても接続が切れない

> **補足**: ping フレームはクライアント → ALB → サーバーと透過される。pong フレームはサーバー → ALB → クライアントと返る。これらのフレームが ALB のカウンタをリセットする。

---

### A-3: ping 間隔が ALB idle timeout より長い場合に ALB が切断する

**何を確認するか**: ping 間隔が ALB idle timeout を超えると、最初の ping 送信前に ALB が切断すること。

- ALB が無通信接続を切断するまでの時間: 60s
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: yes
- ping の送信間隔: 90s（ALB timeout より長い）
- 指定時間後にデータ送信して終了: no

```bash
# alb_idle_timeout=60s に対して 90s 間隔
./client/ws-client -url "$WS_URL" -ping -ping-interval 90s
```

**期待結果 (サーバー)**
- ALB が TCP を切断するため `error: ...` または `disconnected` が出る
- ping は届かない（切断後のため）

**期待結果 (クライアント)**
- 最初の ping 送信（90 秒後）より前の 60 秒後に ALB が切断する
- `read error: websocket: close 1006 (abnormal closure)` が出て終了する
- `[ping] sending at ...` は出力されない

---

### A-4: サーバー idle timeout が ALB idle timeout より短い場合はサーバーが先に切断する

**何を確認するか**: サーバー側の `-idle-timeout` が ALB より短いと、サーバーが先に接続を閉じること。切断の見え方が ALB 切断（`1006`）と異なることを確認する。

- ALB が無通信接続を切断するまでの時間: 60s
- サーバーが無通信接続を閉じるまでの時間: 30s（ALB timeout より短い）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: no

```bash
# EC2 上でサーバーを -idle-timeout 30s で再起動（ALB timeout は 60s）
ssh -i "$KEY_FILE" "$SSH_USER@$(cd terraform && terraform output -raw ec2_public_ip)" \
  "pkill ws-server 2>/dev/null; nohup ./ws-server -idle-timeout 30s > ws-server.log 2>&1 &"

./client/ws-client -url "$WS_URL"
```

**期待結果 (サーバー)**
- 30 秒後に read deadline 超過を検知して WebSocket クローズフレームを送り切断する
- `disconnected` が出る

**期待結果 (クライアント)**
- `read error: websocket: close 1000` または類似のクローズエラーが出る（`1006` ではない）
- ALB の 60 秒に達する前にサーバーが閉じるため `1006` は出ない

---

### A-5: ALB 経由で -send-after によるデータ送受信後に終了する

**何を確認するか**: ALB 越しにエコーが正常に往復し、クライアントが正常終了すること。

- ALB が無通信接続を切断するまでの時間: 60s
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: yes（5s 後、ALB timeout より短い）

```bash
./client/ws-client -url "$WS_URL" -send-after 5s
```

**期待結果 (サーバー)**
- メッセージを受信してエコーを返す
- クライアントからの close フレームを受けて `disconnected` が出る

**期待結果 (クライアント)**
- 5 秒後に `[send] sending data: ...` が出る
- `[recv] ...` でエコーを受信する
- `[send] response received, closing connection` と出力して正常終了する
- データ送信が ALB カウンタをリセットするため、send-after の時間が ALB timeout に近くても切断されない

---

### A-6: サーバーの -no-pong で pong が届かないことを確認する

**何を確認するか**: サーバーが `-no-pong` の場合、クライアントは pong を受信しないこと。ただし ping フレーム自体は ALB を透過してサーバーに届くため、ALB のカウンタはリセットされ接続は維持される。

- ALB が無通信接続を切断するまでの時間: 60s
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: no
- クライアントから定期 ping を送る: yes
- ping の送信間隔: 10s（ALB timeout より短い）
- 指定時間後にデータ送信して終了: no

```bash
# EC2 上でサーバーを -no-pong で再起動
ssh -i "$KEY_FILE" "$SSH_USER@$(cd terraform && terraform output -raw ec2_public_ip)" \
  "pkill ws-server 2>/dev/null; nohup ./ws-server -no-pong > ws-server.log 2>&1 &"

./client/ws-client -url "$WS_URL" -ping -ping-interval 10s
```

**期待結果 (サーバー)**
- `received ping (pong suppressed)` が繰り返し出る
- 接続は維持される

**期待結果 (クライアント)**
- `[pong] received` が出ない
- ping フレームが ALB を透過してカウンタをリセットしているため、ALB による切断は発生しない

---

### A-7: ALB idle timeout の値を変えて再検証する

- ALB が無通信接続を切断するまでの時間: 任意（例: 30s に変更）
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: no

```bash
# terraform/terraform.tfvars を編集して alb_idle_timeout = 30 などに変更
make infra-up        # ALB 設定のみ更新（EC2 は再作成されない）
make client-idle     # 30 秒で切断されることを確認
```

**期待結果 (サーバー)**
- ALB が TCP を切断するため `error: ...` または `disconnected` が出る

**期待結果 (クライアント)**
- 変更後の `alb_idle_timeout` 秒後に `read error: websocket: close 1006 (abnormal closure)` が出て終了する

---

### A-8: ALB とサーバー両方の idle timeout が有効な状態で ping により両方をリセットして維持する

**何を確認するか**: ALB timeout とサーバー timeout が同時に有効なとき、両方より短い間隔で ping を送ると両方のタイマーがリセットされて接続が維持されること。

- ALB が無通信接続を切断するまでの時間: 60s
- サーバーが無通信接続を閉じるまでの時間: 20s（ping 間隔より長く、ALB timeout より短い）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: yes
- ping の送信間隔: 10s（サーバー timeout・ALB timeout の両方より短い）
- 指定時間後にデータ送信して終了: no

```bash
# EC2 上でサーバーを -idle-timeout 20s で起動
ssh -i "$KEY_FILE" "$SSH_USER@$(cd terraform && terraform output -raw ec2_public_ip)" \
  "pkill ws-server 2>/dev/null; nohup ./ws-server -idle-timeout 20s > ws-server.log 2>&1 &"

./client/ws-client -url "$WS_URL" -ping -ping-interval 10s
```

**期待結果 (サーバー)**
- `received ping, sending pong` が 10 秒ごとに繰り返し出る
- ping が read deadline（20s）をリセットし続けるため切断されない

**期待結果 (クライアント)**
- 10 秒ごとに `[ping] sending at ...` と `[pong] received: ...` が出続ける
- 20 秒・60 秒を超えても接続が維持される

---

### A-9: send-after が ALB idle timeout より長い場合、送信前に ALB が切断する

**何を確認するか**: send-after の発火より前に ALB timeout が来て切断されると、クライアントはデータを送らずに終了すること。L-8 の ALB 版。

- ALB が無通信接続を切断するまでの時間: 60s
- サーバーが無通信接続を閉じるまでの時間: なし（無効）
- サーバーが pong を返す: yes
- クライアントから定期 ping を送る: no
- ping の送信間隔: -
- 指定時間後にデータ送信して終了: yes（70s 後、ALB timeout より長い）

```bash
./client/ws-client -url "$WS_URL" -send-after 70s
```

**期待結果 (サーバー)**
- ALB が TCP を切断するため `error: ...` または `disconnected` が出る
- クライアントからのデータは届かない

**期待結果 (クライアント)**
- 60 秒後に `read error: websocket: close 1006 (abnormal closure)` が出る
- send-after（70s 後）が来る前に `case <-done` が選ばれて終了する
- `[send] sending data:` は出力されない

---

### A-10: サーバーログで接続状況を確認する

```bash
make server-log
# または
ssh -i "$KEY_FILE" "$SSH_USER@$(cd terraform && terraform output -raw ec2_public_ip)" \
  "tail -f ws-server.log"
```

---

## ログの見方

| ログ | 意味 |
|---|---|
| `ping disabled — connection will be idle` | クライアントが ping なしで起動した |
| `ping enabled, interval=Xs` | クライアントが ping ありで起動した |
| `[ping] sending at ...` | クライアントが ping フレームを送信した |
| `[pong] received: ...` | クライアントがサーバーから pong を受信した |
| `[send] sending data: ...` | `-send-after` によりデータメッセージを送信した |
| `[send] response received, closing connection` | `-send-after` でエコー受信後に正常終了 |
| `[send] timeout waiting for response` | `-send-after` で 10 秒以内に応答がなかった |
| `received ping, sending pong` | サーバーが ping を受けて pong を返した（サーバー idle timeout もリセット済み）|
| `received ping (pong suppressed)` | サーバーが `-no-pong` で pong を返さなかった（サーバー idle timeout はリセット済み）|
| `close 1006 (abnormal closure)` | **ALB が TCP を強制切断**。WebSocket クローズハンドシェイクなし |
| `close 1000 (normal closure)` | 正常なクローズハンドシェイク（サーバーまたはクライアントが明示的に閉じた）|
