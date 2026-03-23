# ALB WebSocket Idle Timeout 検証

## 事前準備

```bash
# terraform.tfvars を作成（key_name は不要。キーペアは Terraform が自動生成する）
cat > terraform/terraform.tfvars <<EOF
alb_idle_timeout = 60   # 検証したい timeout 値 (秒)
EOF
```

SSH 秘密鍵は `make infra-up` 後に `terraform/id_rsa.pem` へ自動保存される。
デフォルトの `KEY_FILE` は `terraform/id_rsa.pem` を参照するため、通常は変更不要。

## 手順

### 1. インフラ構築

```bash
make infra-up
```

ALB DNS 名・EC2 IP・WebSocket URL が出力されます。

### 2. サーバーデプロイ

```bash
make deploy
```

ビルドと EC2 への転送・起動まで自動で行います。
ALB のヘルスチェックが通るまで約1〜2分待ちます。

### 3. 検証

#### シナリオ A: idle timeout での切断を確認

```bash
make client-idle
```

`alb_idle_timeout` 秒が経過すると ALB が接続を切断します。

#### シナリオ B: ping による接続維持を確認

```bash
# デフォルトは 50s 間隔 (idle_timeout=60s の場合)
make client-ping

# 間隔を変える場合
make client-ping PING_INTERVAL=30s
```

#### シナリオ C: サーバーログを確認

```bash
make server-log
```

### 4. idle timeout を変更して再検証

`terraform/terraform.tfvars` の `alb_idle_timeout` を変更して再適用するだけです:

```bash
# terraform.tfvars を編集後
make infra-up
```

### 5. 後片付け

```bash
make infra-down
```

## Makefile 変数

| 変数 | デフォルト | 説明 |
|---|---|---|
| `KEY_FILE` | `~/.ssh/id_rsa` | SSH 秘密鍵のパス |
| `SSH_USER` | `ec2-user` | EC2 のログインユーザー |
| `PING_INTERVAL` | `50s` | ping 間隔 (`client-ping` 使用時) |

## ログの見方

### クライアント (`client-idle` が切断された場合)

```
2026/01/01 00:00:00 connecting to ws://xxxx.elb.amazonaws.com/ws
2026/01/01 00:00:00 connected
2026/01/01 00:00:00 ping disabled — connection will be idle
2026/01/01 00:01:00 read error: websocket: close 1006 (abnormal closure)
```

`close 1006` = TCP レベルで強制切断 (ALB が idle timeout を検知)

### クライアント (`client-ping` で維持された場合)

```
2026/01/01 00:00:00 connected
2026/01/01 00:00:00 ping enabled, interval=50s
2026/01/01 00:00:50 [ping] sending at 2026-01-01T00:00:50Z
2026/01/01 00:00:50 [pong] received: "2026-01-01T00:00:50Z"
2026/01/01 00:01:40 [ping] sending at 2026-01-01T00:01:40Z
...
```
