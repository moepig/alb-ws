# ALB WebSocket KeepAliveInterval 検証 (ASP.NET Core)

ASP.NET Core の `WebSocketOptions.KeepAliveInterval` を使ったサーバー起点の WebSocket keep-alive が、
ALB の idle timeout に対してどう作用するかを検証する環境。

demo1（Go サーバー、クライアント起点 ping）と対比して確認できる。

## demo1 との違い

| | demo1 | demo2 |
|---|---|---|
| サーバー実装 | Go | ASP.NET Core 10 |
| keep-alive の起点 | クライアントが `-ping` フラグで Ping 送信 | サーバーが `KeepAliveInterval` で Ping 送信し `KeepAliveTimeout` 内の Pong を待つ |
| クライアント | Go (`-ping` フラグ) | Go（idle でも OK; サーバーの Ping に Pong で応答してログ表示） |

## 事前準備

```bash
# terraform.tfvars を作成
cat > terraform/terraform.tfvars <<EOF
alb_idle_timeout = 60   # 検証したい timeout 値 (秒)
EOF
```

SSH 秘密鍵は `make infra-up` 後に `terraform/id_rsa.pem` へ自動保存される。

## 手順

### 1. インフラ構築

```bash
make infra-up
```

### 2. サーバーデプロイ

デフォルトは `KEEP_ALIVE_INTERVAL_SECS=50`（ALB idle timeout 60s より短い）、`KEEP_ALIVE_TIMEOUT_SECS=10`。

```bash
make deploy
# または keep-alive を無効にしてデプロイ
make deploy KEEP_ALIVE_INTERVAL_SECS=0
```

ビルドには .NET 10 SDK が必要（`dotnet publish` で linux-x64 向け self-contained バイナリを生成するため、EC2 に .NET ランタイムは不要）。

ALB ヘルスチェックが通るまで約 1〜2 分待つ。

### 3. 検証

#### シナリオ A: KeepAliveInterval なし → ALB idle timeout で切断

```bash
# サーバーを --keep-alive-interval 0 で再起動
make server-restart KEEP_ALIVE_INTERVAL_SECS=0

# クライアントを idle で接続
make client-idle
```

`alb_idle_timeout` 秒後に ALB が切断する。

#### シナリオ B: KeepAliveInterval あり → 接続を維持

```bash
# サーバーを --keep-alive-interval 50 でデプロイ済みの場合
make client-idle
```

サーバーが 50 秒ごとに Ping を送り、クライアントが Pong で応答することで ALB のカウンタがリセットされ、接続が維持される。
クライアントには `[ping] received: ""` が出力され続ける。

#### シナリオ C: サーバーログを確認

```bash
make server-log
```

### 3b. KeepAliveInterval を変えて再検証

バイナリの再デプロイは不要。`server-restart` で起動オプションだけ変えられる。

```bash
# keep-alive を無効化
make server-restart KEEP_ALIVE_INTERVAL_SECS=0

# 10s 間隔に変更
make server-restart KEEP_ALIVE_INTERVAL_SECS=10
```

### 4. 後片付け

```bash
make infra-down
```

## Makefile 変数

| 変数 | デフォルト | 説明 |
|---|---|---|
| `KEY_FILE` | `terraform/id_rsa.pem` | SSH 秘密鍵のパス |
| `SSH_USER` | `ec2-user` | EC2 のログインユーザー |
| `KEEP_ALIVE_INTERVAL_SECS` | `50` | サーバーの KeepAliveInterval (秒, 0 で無効) |
| `KEEP_ALIVE_TIMEOUT_SECS` | `10` | サーバーの KeepAliveTimeout (秒, Ping 送信後 Pong が返らなければ切断) |

## ローカル動作確認

```bash
# サーバー + クライアントを tmux で同時起動
make dev-ka          # KeepAliveInterval=50s で起動
make dev-idle        # KeepAliveInterval=0 (無効) で起動
```

または別ターミナルで:

```bash
make run-server
# 別ターミナルで
make run-client-idle
```

## ログの見方

### クライアント (KeepAlive なしで切断された場合)

```
2026/01/01 00:00:00 connecting to ws://xxxx.elb.amazonaws.com/ws
2026/01/01 00:00:00 connected — server keep-alive handles idle
2026/01/01 00:01:00 read error: websocket: close 1006 (abnormal closure)
```

### クライアント (KeepAlive で維持された場合)

`KeepAliveInterval` + `KeepAliveTimeout` が設定されているとき、サーバーは Ping を送信する。クライアントは Pong で応答し `[ping] received` をログ出力する。

```
2026/01/01 00:00:00 connecting to ws://xxxx.elb.amazonaws.com/ws
2026/01/01 00:00:00 connected — server keep-alive handles idle
2026/01/01 00:00:50 [ping] received: "\x00\x00\x00\x00\x00\x00\x00\x01"
2026/01/01 00:01:40 [ping] received: "\x00\x00\x00\x00\x00\x00\x00\x02"
# 以降 50 秒ごとに出続ける
```

### サーバー

```
yyyy/MM/dd HH:mm:ss info: server listening on http://0.0.0.0:8080 (keep-alive-interval=50s, keep-alive-timeout=10s, idle-timeout=0s)
yyyy/MM/dd HH:mm:ss info: [::1] connected
```
