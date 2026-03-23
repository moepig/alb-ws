# ---- 設定 ----
KEY_FILE                 ?= terraform/id_rsa.pem
SSH_USER                 ?= ec2-user
KEEP_ALIVE_INTERVAL_SECS ?= 50
KEEP_ALIVE_TIMEOUT_SECS  ?= 10

# ---- ローカル設定 ----
LOCAL_URL               ?= ws://localhost:8080/ws
LOCAL_IDLE_TIMEOUT_SECS ?= 0

# ---- terraform output から自動取得 ----
EC2_IP = $(shell cd terraform && terraform output -raw ec2_public_ip 2>/dev/null)
WS_URL = $(shell cd terraform && terraform output -raw websocket_url 2>/dev/null)

.PHONY: build build-server build-client build-client2 \
        test test-server test-client test-client2 \
        dev-idle dev-ka \
        run-server run-client-idle run-client2-idle \
        infra-up infra-down \
        deploy server-restart server-log \
        client-idle client2-idle

# ---- テスト ----
test: test-server test-client test-client2

test-server:
	dotnet test server/WebSocketServer.sln --verbosity normal

test-client:
	cd client && go test -v ./...

test-client2:
	cd client2 && go test -v ./...

# ---- ローカル同時起動 (tmux) ----
# 左ペイン: サーバー  右ペイン: クライアント
# Ctrl-C で両方終了
dev-idle:
	tmux new-session \
	  "cd server/WebSocketServer && dotnet run -- --keep-alive-interval 0 --idle-timeout $(LOCAL_IDLE_TIMEOUT_SECS)" \; \
	  split-window -h \
	  "sleep 3 && cd client && go run . -url $(LOCAL_URL); read"

dev-ka:
	tmux new-session \
	  "cd server/WebSocketServer && dotnet run -- --keep-alive-interval $(KEEP_ALIVE_INTERVAL_SECS) --keep-alive-timeout $(KEEP_ALIVE_TIMEOUT_SECS)" \; \
	  split-window -h \
	  "sleep 3 && cd client && go run . -url $(LOCAL_URL); read"


# ---- ローカル実行 ----
# 別ターミナルで run-server を起動してから run-client-idle を実行する
run-server:
	cd server/WebSocketServer && dotnet run -- --keep-alive-interval $(KEEP_ALIVE_INTERVAL_SECS) --keep-alive-timeout $(KEEP_ALIVE_TIMEOUT_SECS) --idle-timeout $(LOCAL_IDLE_TIMEOUT_SECS)

run-client-idle:
	cd client && go run . -url $(LOCAL_URL)

run-client2-idle:
	cd client2 && go run . -url $(LOCAL_URL)


# ---- ビルド ----
build: build-server build-client build-client2

build-server:
	dotnet publish server/WebSocketServer -r linux-x64 -c Release --self-contained -o server/WebSocketServer/out

build-client:
	cd client && go build -o ws-client .

build-client2:
	cd client2 && go build -o ws-client2 .

# ---- インフラ ----
infra-up:
	cd terraform && terraform init -input=false && terraform apply

infra-down:
	cd terraform && terraform destroy

# ---- サーバーデプロイ ----
deploy: build-server
	scp -i $(KEY_FILE) server/WebSocketServer/out/ws-server $(SSH_USER)@$(EC2_IP):/home/$(SSH_USER)/
	ssh -i $(KEY_FILE) $(SSH_USER)@$(EC2_IP) \
	  "chmod +x ws-server; pkill ws-server 2>/dev/null; nohup ./ws-server --keep-alive-interval $(KEEP_ALIVE_INTERVAL_SECS) --keep-alive-timeout $(KEEP_ALIVE_TIMEOUT_SECS) > ws-server.log 2>&1 & echo 'server started'"

server-restart:
	ssh -i $(KEY_FILE) $(SSH_USER)@$(EC2_IP) \
	  "pkill ws-server 2>/dev/null; nohup ./ws-server --keep-alive-interval $(KEEP_ALIVE_INTERVAL_SECS) --keep-alive-timeout $(KEEP_ALIVE_TIMEOUT_SECS) > ws-server.log 2>&1 & echo 'server restarted'"

server-log:
	ssh -i $(KEY_FILE) $(SSH_USER)@$(EC2_IP) "tail -f ws-server.log"

# ---- クライアント実行 ----
# ALB 経由で接続（クライアントは ping を送らない; サーバーの KeepAlive に依存）
client-idle:
	./client/ws-client -url $(WS_URL)

# TCP RST で切断するクライアント（WebSocket/TCP close handshake なし）
client2-idle:
	./client2/ws-client2 -url $(WS_URL)

