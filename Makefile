# ---- 設定 ----
KEY_FILE      ?= ~/.ssh/id_rsa
SSH_USER      ?= ec2-user
PING_INTERVAL ?= 50s

# ---- ローカル設定 ----
LOCAL_URL          ?= ws://localhost:8080/ws
LOCAL_IDLE_TIMEOUT ?= 60s

# ---- terraform output から自動取得 ----
EC2_IP = $(shell cd terraform && terraform output -raw ec2_public_ip 2>/dev/null)
WS_URL = $(shell cd terraform && terraform output -raw websocket_url 2>/dev/null)

.PHONY: build build-server build-client \
        test test-server test-client \
        dev-idle dev-ping \
        run-server run-client-idle run-client-ping \
        infra-up infra-down \
        deploy server-log \
        client-idle client-ping

# ---- テスト ----
test: test-server test-client

test-server:
	cd server && go test -v ./...

test-client:
	cd client && go test -v ./...

# ---- ローカル同時起動 (tmux) ----
# 左ペイン: サーバー  右ペイン: クライアント
# Ctrl-C で両方終了
dev-idle:
	tmux new-session \
	  "cd server && go run . -idle-timeout $(LOCAL_IDLE_TIMEOUT)" \; \
	  split-window -h \
	  "sleep 1 && cd client && go run . -url $(LOCAL_URL); read"

dev-ping:
	tmux new-session \
	  "cd server && go run . -idle-timeout $(LOCAL_IDLE_TIMEOUT)" \; \
	  split-window -h \
	  "sleep 1 && cd client && go run . -url $(LOCAL_URL) -ping -ping-interval $(PING_INTERVAL); read"

# ---- ローカル実行 ----
# 別ターミナルで run-server を起動してから run-client-* を実行する
run-server:
	cd server && go run . -idle-timeout $(LOCAL_IDLE_TIMEOUT)

run-client-idle:
	cd client && go run . -url $(LOCAL_URL)

run-client-ping:
	cd client && go run . -url $(LOCAL_URL) -ping -ping-interval $(PING_INTERVAL)

# ---- ビルド ----
build: build-server build-client

build-server:
	cd server && go build -o ws-server .

build-client:
	cd client && go build -o ws-client .

# ---- インフラ ----
infra-up:
	cd terraform && terraform init -input=false && terraform apply

infra-down:
	cd terraform && terraform destroy

# ---- サーバーデプロイ ----
deploy: build-server
	scp -i $(KEY_FILE) server/ws-server $(SSH_USER)@$(EC2_IP):/home/$(SSH_USER)/
	ssh -i $(KEY_FILE) $(SSH_USER)@$(EC2_IP) \
	  "pkill ws-server 2>/dev/null; nohup ./ws-server > ws-server.log 2>&1 & echo 'server started'"

server-log:
	ssh -i $(KEY_FILE) $(SSH_USER)@$(EC2_IP) "tail -f ws-server.log"

# ---- クライアント実行 ----
# ping なし: ALB idle timeout で切断されることを確認
client-idle:
	./client/ws-client -url $(WS_URL)

# ping あり: PING_INTERVAL 間隔で ping を送信
client-ping:
	./client/ws-client -url $(WS_URL) -ping -ping-interval $(PING_INTERVAL)
