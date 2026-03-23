package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	url := flag.String("url", "ws://localhost:8080/ws", "WebSocket server URL")
	pingEnabled := flag.Bool("ping", false, "send WebSocket ping frames")
	pingInterval := flag.Duration("ping-interval", 30*time.Second, "interval between pings (e.g. 10s, 1m)")
	sendAfter := flag.Duration("send-after", 0, "send a data message after this duration and exit on response (e.g. 5s); requires -ping=false")
	flag.Parse()

	log.Printf("connecting to %s", *url)

	conn, _, err := websocket.DefaultDialer.Dial(*url, nil)
	if err != nil {
		log.Fatalf("dial error: %v", err)
	}
	defer conn.Close()

	log.Printf("connected")

	conn.SetPongHandler(func(data string) error {
		log.Printf("[pong] received: %q", data)
		return nil
	})

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	done := make(chan struct{})
	recv := make(chan struct{}, 1)

	go func() {
		defer close(done)
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("read error: %v", err)
				return
			}
			log.Printf("[recv] type=%d msg=%s", msgType, string(msg))
			select {
			case recv <- struct{}{}:
			default:
			}
		}
	}()

	var pingTickerC <-chan time.Time
	if *pingEnabled {
		ticker := time.NewTicker(*pingInterval)
		defer ticker.Stop()
		pingTickerC = ticker.C
		log.Printf("ping enabled, interval=%s", *pingInterval)
	} else {
		log.Printf("ping disabled — connection will be idle")
	}

	var sendAfterC <-chan time.Time
	if *sendAfter > 0 {
		if *pingEnabled {
			log.Fatalf("-send-after requires -ping=false")
		}
		sendAfterC = time.After(*sendAfter)
		log.Printf("will send data after %s and exit on response", *sendAfter)
	}

	for {
		select {
		case <-done:
			return

		case t := <-pingTickerC:
			payload := t.Format(time.RFC3339)
			log.Printf("[ping] sending at %s", payload)
			deadline := time.Now().Add(10 * time.Second)
			if err := conn.WriteControl(websocket.PingMessage, []byte(payload), deadline); err != nil {
				log.Printf("[ping] error: %v", err)
				return
			}

		case t := <-sendAfterC:
			sendAfterC = nil
			payload := t.Format(time.RFC3339)
			log.Printf("[send] sending data: %q", payload)
			if err := conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
				log.Printf("[send] error: %v", err)
				return
			}
			log.Printf("[send] waiting for response...")
			select {
			case <-recv:
				log.Printf("[send] response received, closing connection")
			case <-time.After(10 * time.Second):
				log.Printf("[send] timeout waiting for response")
			case <-done:
				return
			}
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			conn.WriteMessage(websocket.CloseMessage, msg) //nolint:errcheck
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return

		case <-interrupt:
			log.Printf("interrupted, closing connection")
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			if err := conn.WriteMessage(websocket.CloseMessage, msg); err != nil {
				log.Printf("close write error: %v", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
