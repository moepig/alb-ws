# テストケース一覧

## KeepAliveInterval / KeepAliveTimeout とは

`WebSocketOptions.KeepAliveInterval` と `WebSocketOptions.KeepAliveTimeout` を**両方**設定すると、ASP.NET Core が PING/PONG モードで動作する。

- サーバーは `KeepAliveInterval` 間隔で **Ping フレーム (opcode 0x9)** を送信する
- クライアントが `KeepAliveTimeout` 内に Pong を返さなければ、サーバーは接続を切断する
- この Ping/Pong トラフィックが ALB のアイドルタイムアウトカウンタをリセットする

`KeepAliveTimeout` を設定しない場合（デフォルト `Timeout.InfiniteTimeSpan`）はタイムアウトなしの unsolicited Pong 送信になる。

### demo1 との対比

| | demo1 | demo2 |
|---|---|---|
| keep-alive フレームの送信元 | クライアント (`-ping` フラグで Ping 送信) | サーバー (`KeepAliveInterval` で Ping 送信) |
| 応答フレーム | サーバーが Pong を返す | クライアントが Pong を返す |
| 無応答時の挙動 | サーバーは検知しない | `KeepAliveTimeout` 後にサーバーが接続を切断 |
| ALB カウンタリセット | Ping + Pong の両方向トラフィックでリセット | 同上 |
| クライアントログ | `[ping] sending at ...` / `[pong] received: ...` | `[ping] received: ""` |

### アイドルタイムアウトの種類

このプロジェクトには **3 種類のタイムアウト** が存在する。

#### 1. ALB アイドルタイムアウト

| 項目 | 内容 |
|---|---|
| 設定箇所 | `terraform/terraform.tfvars` の `alb_idle_timeout`（秒） |
| 適用範囲 | AWS 環境のみ |
| 動作 | TCP レイヤーのデータが指定秒間流れなければ ALB が強制切断 |
| 切断の見え方 | `close 1006 (abnormal closure)` |
| リセット条件 | ping/pong/データメッセージ（どちら向きでも）でリセット |

#### 2. ASP.NET Core KeepAliveInterval / KeepAliveTimeout

| 項目 | 内容 |
|---|---|
| 設定箇所 | `--keep-alive-interval` フラグ（秒、0 で無効）/ `--keep-alive-timeout` フラグ（秒） |
| 適用範囲 | ローカル・AWS 両方 |
| 動作 | サーバーが `KeepAliveInterval` 間隔で Ping フレームを送信。`KeepAliveTimeout` 内に Pong が返らなければ接続を切断 |
| クライアントから見た挙動 | gorilla/websocket の `SetPingHandler` が呼ばれ、Pong を返送しつつ `[ping] received: ""` をログ出力する |

#### 3. サーバー実装のアイドルタイムアウト

| 項目 | 内容 |
|---|---|
| 設定箇所 | `--idle-timeout` フラグ（秒、0 で無効） |
| 適用範囲 | ローカル・AWS 両方 |
| 動作 | アプリケーション層のメッセージが指定秒間届かなければサーバーが接続を閉じる |
| 注意 | KeepAlive の Pong フレームはフレームワーク内で処理され `ReceiveAsync` には上がらないため、このタイムアウトはリセットされない |

---

## クライアント消滅時の挙動 (ALB 経由)

### TCP コネクションの構造

ALB はリバースプロキシとして動作し、TCP コネクションを 2 本維持する。

```
Client ──[TCP conn A]── ALB ──[TCP conn B]── Server
```

conn A と conn B は**独立した TCP コネクション**。クライアントの死は自動的にサーバー側に伝播しない。

---

### パターン 1: クライアントが FIN / RST を送って終了

プロセス終了・`client2`（SO_LINGER=0）など TCP レベルの通知が届く場合。

```
T+0ms   Client → ALB: RST (または FIN)
T+1ms   ALB: conn A を close
T+1ms   ALB → Server: conn B に RST (または FIN) を送信
T+2ms   Server: ReceiveAsync がエラーで返る
        "The remote party closed the WebSocket connection
         without completing the close handshake."
        サーバーのコネクションは即座に解放される
```

---

### パターン 2: クライアントホストがクラッシュ (無音消滅)

ネットワーク切断・電源断など FIN/RST が届かない場合。KeepAlive 設定によって挙動が大きく変わる。

#### KeepAlive なし (`--keep-alive-interval 0`)

```
T=0s    Client 消滅 (無音)
        conn A / conn B は両方 open のまま
        ReceiveAsync は永遠にブロックしたまま

T=Xs    ALB idle timeout 経過 (X = alb_idle_timeout)
        ALB → Server: conn B に RST を送信
T=Xs    Server: ReceiveAsync がエラーで返る
        サーバーのコネクション解放
```

サーバーのコネクションは `alb_idle_timeout` 秒間残り続ける。

---

#### KeepAliveInterval のみ設定 (unsolicited PONG、KeepAliveTimeout なし)

```
T=0s    Client 消滅 (無音)

T=50s   Server → ALB → Client: unsolicited PONG 送信
        ┌──────────────────────────────────────────────────────┐
        │ Server は PONG への応答を期待しない                   │
        │ → サーバーはこの時点で異常を検知できない              │
        └──────────────────────────────────────────────────────┘
        ALB idle timer リセット (送信トラフィックが発生したため)
        conn A: クライアントへの送信に TCP ACK が返らない
        → TCP retransmission 開始

T=100s  Server: 次の unsolicited PONG 送信
        ALB idle timer が再びリセット
        (以降 PONG を送るたびに繰り返す)

        PONG が ALB idle timer をリセットし続けるため
        → ALB idle timeout は事実上発火しない

Eventually  OS の TCP retransmission timeout に到達
        Linux デフォルト: 約 15 分
        Windows デフォルト: 約 21 秒
        ALB: conn A の TCP 障害を検知 → conn B を close
        Server: ReceiveAsync がエラーで返る、コネクション解放
```

**KeepAlive なしより悪化しうる。** ALB idle timeout を無効化しながらサーバーは検知できないため、ゾンビコネクションが OS の TCP retransmission timeout まで残り続ける可能性がある。

---

#### KeepAliveInterval + KeepAliveTimeout 両方設定 (PING/PONG)

`KeepAliveInterval=50s`、`KeepAliveTimeout=10s`、`alb_idle_timeout=60s` の場合。

```
T=0s    Client 消滅 (無音)

T=50s   Server → ALB → Client: PING 送信
        ALB idle timer リセット
        クライアントは死んでいるため PONG が返らない

T=60s   KeepAliveTimeout 経過 (50s + 10s)
        Server: PONG 未着を検知
        Server → ALB: conn B を close (RST/FIN)
        ALB: conn B close を検知 → conn A も close
        サーバーのコネクション解放
```

最大 `KeepAliveInterval + KeepAliveTimeout` 秒でサーバーが自ら検知・解放する。

---

### KeepAlive 設定別まとめ

| 設定 | 無音消滅時のサーバーコネクション残存時間 | 検知の起点 |
|---|---|---|
| KeepAlive なし | `alb_idle_timeout` 秒 | ALB idle timeout |
| interval のみ (unsolicited PONG) | OS TCP retransmission timeout (最大 15 分超) | ALB の TCP 障害検知 ※PONG が ALB idle timer をリセットし続けるため idle timeout が機能しない |
| interval + timeout (PING/PONG) | `KeepAliveInterval + KeepAliveTimeout` 秒 | サーバーの PONG 未受信 |
| interval > `alb_idle_timeout` | `alb_idle_timeout` 秒 | ALB idle timeout が先に発火 |
| FIN / RST あり (いずれの設定でも) | ほぼ 0 ms | TCP 切断が ALB 経由で伝播 |

### TCP keepalive との関係

OS レベルの TCP keepalive (WebSocket keepalive とは別) も存在するが、デフォルト値は実用的でない。

| OS | 初回 keepalive まで | その後 | 合計 |
|---|---|---|---|
| Linux | 7200 秒 | 9 回 × 75 秒 | 約 2 時間 |
| Windows | 7200 秒 | 10 回 × 1 秒 | 約 2 時間 |

WebSocket レベルの `KeepAliveInterval + KeepAliveTimeout` を適切に設定することで、OS keepalive よりはるかに早くゾンビコネクションを回収できる。

---

## ローカル

ALB は介在しないため ALB タイムアウトは動作しない。
**KeepAliveInterval** と**サーバー実装のアイドルタイムアウト**のみが動作する。

### 前準備

```bash
make build
```

---

### L-1: KeepAliveInterval なし + サーバーアイドルタイムアウトでアイドル接続が切断される

**何を確認するか**: `--keep-alive-interval 0` でサーバーが Pong を送らない状態で、`--idle-timeout` がアイドル接続を閉じること。

- KeepAliveInterval: 無効
- サーバー idle timeout: 10s
- クライアントから ping: no

**ターミナル 1 (サーバー)**
```bash
./server/out/ws-server --keep-alive-interval 0 --idle-timeout 10
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws
```

**期待結果 (サーバー)**
- 10 秒後に idle timeout を検知して接続を閉じる

**期待結果 (クライアント)**
- `read error: ...` が出て終了する

---

### L-2: KeepAliveInterval あり → サーバーアイドルタイムアウトが来ない

**何を確認するか**: `KeepAliveInterval` によるサーバー Pong が ALB カウンタと同様に機能し、keep-alive として動作すること。ただしローカルでは ALB がないため、サーバー側のアイドルタイムアウトへの影響を確認する。

注意: `KeepAliveInterval` の Pong フレームはフレームワーク内で処理され `ReceiveAsync` には上がらないため、サーバー実装の `--idle-timeout` はリセットされない。`--idle-timeout 0`（無効）で動作確認すること。

- KeepAliveInterval: 5s
- サーバー idle timeout: 無効 (0)
- クライアントから ping: no

**ターミナル 1 (サーバー)**
```bash
./server/out/ws-server --keep-alive-interval 5
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws
```

**期待結果 (サーバー)**
- サーバーが 5 秒ごとに Ping を送信（ログには直接出ない）
- 接続が維持される

**期待結果 (クライアント)**
- 接続が切れない
- 5 秒ごとに `[ping] received: "\x00\x00\x00\x00\x00\x00\x00\x01"` など（カウンタがインクリメント）が出力され続ける

---

### L-3: KeepAliveInterval あり + サーバーアイドルタイムアウトの組み合わせ

**何を確認するか**: KeepAlive Pong はサーバー実装の `--idle-timeout` をリセットしないことを確認する。

- KeepAliveInterval: 5s
- サーバー idle timeout: 10s
- クライアントから ping: no

**ターミナル 1 (サーバー)**
```bash
./server/out/ws-server --keep-alive-interval 5 --idle-timeout 10
```

**ターミナル 2 (クライアント)**
```bash
./client/ws-client -url ws://localhost:8080/ws
```

**期待結果 (サーバー)**
- KeepAlive の Ping/Pong はフレームワーク内で処理され `ReceiveAsync` には上がらないため、idle timeout の CancellationToken はリセットされない
- 10 秒後に idle timeout で接続を閉じる

**期待結果 (クライアント)**
- 5 秒ごとに `[ping] received: ""` が出力される
- 10 秒後にサーバーが閉じるため `read error: ...` が出て終了する

---

### L-4: -send-after でデータ送信・応答確認後に終了する

- KeepAliveInterval: 30s（デフォルト）
- サーバー idle timeout: 無効
- クライアント: -send-after 3s

**ターミナル 1 (サーバー)**
```bash
./server/out/ws-server
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
- エコーを受信して正常終了する

---

### L-5: クライアントが TCP RST で切断した場合のサーバーログ

**何を確認するか**: `client2`（WebSocket/TCP close handshake なしで終了）が切断したとき、サーバーが close handshake 未完了のエラーをログ出力すること。

**ターミナル 1 (サーバー)**
```bash
./server/WebSocketServer/out/ws-server --keep-alive-interval 0
```

**ターミナル 2 (クライアント)**
```bash
./client2/ws-client2 -url ws://localhost:8080/ws -send-after 3s
```

**期待結果 (サーバー)**
```
yyyy/MM/dd HH:mm:ss fail: WebSocketHandler[0]
      [127.0.0.1] error: The remote party closed the WebSocket connection without completing the close handshake.
yyyy/MM/dd HH:mm:ss info: Microsoft.AspNetCore.Routing.EndpointMiddleware[1]
      Executed endpoint '/ws'
```

**期待結果 (クライアント)**
```
2026/01/01 00:00:00 connecting to ws://localhost:8080/ws
2026/01/01 00:00:00 connected — will close abruptly (RST, no WebSocket close handshake)
2026/01/01 00:00:03 [send] sending data: "..."
2026/01/01 00:00:03 [send] response received, closing abruptly
2026/01/01 00:00:03 read error: read tcp ...: use of closed network connection
```

`use of closed network connection` は `abruptClose()` で TCP を閉じた後に read goroutine が検知するもので想定内。

---

### L-6: 単体テスト

```bash
make test
```

または個別に:

```bash
make test-server   # dotnet test server/WebSocketServer.sln
make test-client   # cd client && go test -v ./...
```

---

## AWS (ALB 経由)

事前に `make infra-up` と `make deploy` が完了していること。

```bash
export WS_URL=$(cd terraform && terraform output -raw websocket_url)
```

---

### A-1: KeepAliveInterval なし → ALB idle timeout で切断

**何を確認するか**: サーバーが Pong を送らない場合、ALB idle timeout だけが切断源になること。

- KeepAliveInterval: 無効 (0)
- ALB idle timeout: 60s

```bash
make server-restart KEEP_ALIVE_INTERVAL_SECS=0
make client-idle
```

**期待結果 (クライアント)**
- 60 秒後に `read error: websocket: close 1006 (abnormal closure)` が出て終了する

---

### A-2: KeepAliveInterval あり → ALB idle timeout を回避し接続を維持

**何を確認するか**: サーバーの `KeepAliveInterval` Pong が ALB のカウンタをリセットし、`alb_idle_timeout` を超えても接続が維持されること。

- KeepAliveInterval: 50s（ALB timeout より短い）
- ALB idle timeout: 60s

```bash
make server-restart KEEP_ALIVE_INTERVAL_SECS=50
make client-idle
```

**期待結果 (クライアント)**
- 60 秒を超えても接続が切れない
- 50 秒ごとに `[ping] received: "\x00\x00\x00\x00\x00\x00\x00\x01"` など（カウンタがインクリメント）が出力され続ける

---

### A-3: KeepAliveInterval が ALB idle timeout より長い場合に ALB が切断

**何を確認するか**: Pong 送信間隔が ALB idle timeout を超えると、最初の Pong 送信前に ALB が切断すること。

- KeepAliveInterval: 90s（ALB timeout より長い）
- ALB idle timeout: 60s

```bash
make server-restart KEEP_ALIVE_INTERVAL_SECS=90
make client-idle
```

**期待結果 (クライアント)**
- 60 秒後（最初の Pong 送信前）に `read error: websocket: close 1006 (abnormal closure)` が出る

---

### A-4: ALB idle timeout の値を変えて再検証

```bash
# terraform/terraform.tfvars を編集して alb_idle_timeout = 30 などに変更
make infra-up
make server-restart KEEP_ALIVE_INTERVAL_SECS=20  # 30s より短い間隔
make client-idle
```

**期待結果 (クライアント)**
- 30 秒を超えても接続が維持される

---

### A-5: サーバーログで接続状況を確認する

```bash
make server-log
```

---

## ログの見方

| ログ | 意味 |
|---|---|
| `connected — server keep-alive handles idle` | クライアントが接続し、サーバーの KeepAlive に依存する状態 |
| `connected — will close abruptly (RST, ...)` | client2 が接続した（abrupt close モード） |
| `[ping] received: "\x00\x00\x00\x00\x00\x00\x00\x01"` | サーバーから Ping を受信した（Pong を返送済み）。データは ASP.NET Core が付与する 8 バイト big-endian カウンタ |
| `[pong] received: ""` | サーバーから unsolicited Pong を受信した（`KeepAliveTimeout` 未設定時） |
| `[send] sending data: ...` | `-send-after` によりデータメッセージを送信した |
| `[send] response received, closing connection` | エコー受信後に正常終了（client） |
| `[send] response received, closing abruptly` | エコー受信後に TCP RST で終了（client2） |
| `read error: read tcp ...: use of closed network connection` | abruptClose 後に read goroutine が検知。想定内 |
| `close 1006 (abnormal closure)` | **ALB が TCP を強制切断**。KeepAlive が機能していない |
| `close 1000 (normal closure)` | 正常なクローズハンドシェイク |

### KeepAlive が機能しているときのクライアントログ

`KeepAliveInterval` + `KeepAliveTimeout` が設定されている場合、サーバーは Ping を送信する。
データペイロードは ASP.NET Core が内部で管理する 8 バイト big-endian カウンタ（1, 2, 3, ...）。

```
2026/01/01 00:00:00 connected — server keep-alive handles idle
2026/01/01 00:00:50 [ping] received: "\x00\x00\x00\x00\x00\x00\x00\x01"
2026/01/01 00:01:40 [ping] received: "\x00\x00\x00\x00\x00\x00\x00\x02"
2026/01/01 00:02:30 [ping] received: "\x00\x00\x00\x00\x00\x00\x00\x03"
# 以降 50 秒ごとに出続ける
```
