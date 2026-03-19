package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// echoServer starts a test WebSocket server that echoes messages and responds to pings.
func echoServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetPingHandler(func(data string) error {
			return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
		})
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	return srv, wsURL
}

func TestClientConnect(t *testing.T) {
	srv, wsURL := echoServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
}

func TestClientPongHandlerCalled(t *testing.T) {
	srv, wsURL := echoServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	pongReceived := make(chan string, 1)
	conn.SetPongHandler(func(data string) error {
		pongReceived <- data
		return nil
	})

	payload := time.Now().Format(time.RFC3339)
	if err := conn.WriteControl(websocket.PingMessage, []byte(payload), time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Drive the read loop so control frame handlers are called
	go func() {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		conn.ReadMessage()
	}()

	select {
	case got := <-pongReceived:
		if got != payload {
			t.Errorf("pong payload: expected %q, got %q", payload, got)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for pong")
	}
}

func TestClientReceivesEcho(t *testing.T) {
	srv, wsURL := echoServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	want := "test message from client"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != want {
		t.Errorf("echo: expected %q, got %q", want, string(got))
	}
}

func TestClientNormalClose(t *testing.T) {
	srv, wsURL := echoServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	if err := conn.WriteMessage(websocket.CloseMessage, closeMsg); err != nil {
		t.Fatalf("close write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Error("expected error after close, got none")
	}
}

func TestClientSendAfterSendsAndReceivesEcho(t *testing.T) {
	srv, wsURL := echoServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Simulate -send-after: wait the delay, send a message, wait for echo, then close.
	sendAfter := 50 * time.Millisecond
	recv := make(chan string, 1)

	go func() {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		recv <- string(msg)
	}()

	time.Sleep(sendAfter)
	payload := time.Now().Format(time.RFC3339)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case got := <-recv:
		if got != payload {
			t.Errorf("echo: expected %q, got %q", payload, got)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for echo after send-after")
	}

	// Verify clean close
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	if err := conn.WriteMessage(websocket.CloseMessage, closeMsg); err != nil {
		t.Fatalf("close write: %v", err)
	}
}

func TestClientPingInterval(t *testing.T) {
	srv, wsURL := echoServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	pongCount := 0
	pongCh := make(chan struct{}, 3)
	conn.SetPongHandler(func(data string) error {
		pongCh <- struct{}{}
		return nil
	})

	// Simulate ticker-driven pings at short interval
	go func() {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	interval := 50 * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.After(300 * time.Millisecond)

	for {
		select {
		case t := <-ticker.C:
			payload := t.Format(time.RFC3339Nano)
			conn.WriteControl(websocket.PingMessage, []byte(payload), time.Now().Add(5*time.Second))
		case <-pongCh:
			pongCount++
		case <-deadline:
			if pongCount == 0 {
				t.Error("no pongs received during interval test")
			}
			return
		}
	}
}
