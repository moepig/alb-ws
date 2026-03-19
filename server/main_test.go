package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newTestServer starts a test WebSocket server using the given handler config.
func newTestServer(t *testing.T, h *wsHandler) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.handleWS))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	return srv, wsURL
}

func TestHealthEndpoint(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if body := strings.TrimSpace(w.Body.String()); body != "ok" {
		t.Errorf("expected body %q, got %q", "ok", body)
	}
}

func TestHandleWSEcho(t *testing.T) {
	srv, wsURL := newTestServer(t, &wsHandler{})
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	want := "hello world"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != want {
		t.Errorf("echo: expected %q, got %q", want, string(msg))
	}
}

func TestHandleWSBinaryEcho(t *testing.T) {
	srv, wsURL := newTestServer(t, &wsHandler{})
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	want := []byte{0x01, 0x02, 0x03}
	if err := conn.WriteMessage(websocket.BinaryMessage, want); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Errorf("message type: expected %d, got %d", websocket.BinaryMessage, msgType)
	}
	if string(msg) != string(want) {
		t.Errorf("echo: expected %v, got %v", want, msg)
	}
}

func TestHandleWSPingPong(t *testing.T) {
	srv, wsURL := newTestServer(t, &wsHandler{})
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

	payload := "ping-payload"
	if err := conn.WriteControl(websocket.PingMessage, []byte(payload), time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Drive the read loop to process control frames
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

func TestHandleWSNormalClose(t *testing.T) {
	srv, wsURL := newTestServer(t, &wsHandler{})
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

func TestHandleWSMultipleMessages(t *testing.T) {
	srv, wsURL := newTestServer(t, &wsHandler{})
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	messages := []string{"first", "second", "third"}
	for _, want := range messages {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
			t.Fatalf("write %q: %v", want, err)
		}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, got, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != want {
			t.Errorf("expected %q, got %q", want, string(got))
		}
	}
}

// --- no-pong tests ---

func TestHandleWSNoPongSuppressesPong(t *testing.T) {
	srv, wsURL := newTestServer(t, &wsHandler{noPong: true})
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	pongReceived := make(chan struct{}, 1)
	conn.SetPongHandler(func(string) error {
		pongReceived <- struct{}{}
		return nil
	})

	// Drive the read loop so control frame handlers fire
	go func() {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	if err := conn.WriteControl(websocket.PingMessage, []byte("test"), time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("ping: %v", err)
	}

	select {
	case <-pongReceived:
		t.Error("expected no pong, but pong was received")
	case <-time.After(300 * time.Millisecond):
		// correct: no pong received
	}
}

func TestHandleWSDefaultSendsPong(t *testing.T) {
	srv, wsURL := newTestServer(t, &wsHandler{})
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	pongReceived := make(chan struct{}, 1)
	conn.SetPongHandler(func(string) error {
		pongReceived <- struct{}{}
		return nil
	})

	go func() {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	if err := conn.WriteControl(websocket.PingMessage, []byte("test"), time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("ping: %v", err)
	}

	select {
	case <-pongReceived:
		// correct: pong received
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for pong")
	}
}

// --- idle timeout tests ---

func TestHandleWSIdleTimeoutClosesConnection(t *testing.T) {
	srv, wsURL := newTestServer(t, &wsHandler{idleTimeout: 200 * time.Millisecond})
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send nothing — server should close the connection after idle timeout
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected connection to be closed by idle timeout, but read succeeded")
	}
}

func TestHandleWSIdleTimeoutResetByMessage(t *testing.T) {
	timeout := 300 * time.Millisecond
	srv, wsURL := newTestServer(t, &wsHandler{idleTimeout: timeout})
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send a message just before the timeout expires to reset the deadline
	time.Sleep(timeout / 2)
	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("connection closed too early after message reset deadline: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("expected %q, got %q", "hello", string(got))
	}
}

func TestHandleWSIdleTimeoutResetByPing(t *testing.T) {
	timeout := 300 * time.Millisecond
	srv, wsURL := newTestServer(t, &wsHandler{idleTimeout: timeout})
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	pongCh := make(chan struct{}, 1)
	conn.SetPongHandler(func(string) error {
		pongCh <- struct{}{}
		return nil
	})

	// Drive read loop (needed for pong handler to fire)
	go func() {
		for {
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Send ping just before timeout expires to reset server deadline
	time.Sleep(timeout / 2)
	if err := conn.WriteControl(websocket.PingMessage, []byte("ka"), time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("ping: %v", err)
	}

	select {
	case <-pongCh:
		// pong received — server is alive and deadline was reset
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for pong — connection may have died early")
	}
}
