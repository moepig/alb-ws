package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

// abruptClose closes the TCP connection immediately by sending RST (SO_LINGER=0)
// without sending a WebSocket Close frame or performing a TCP FIN/ACK handshake.
func abruptClose(conn *websocket.Conn) {
	if tc, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
		tc.SetLinger(0) //nolint:errcheck
	}
	conn.Close() //nolint:errcheck
}

func main() {
	url       := flag.String("url", "ws://localhost:8080/ws", "WebSocket server URL")
	sendAfter := flag.Duration("send-after", 0, "send a data message after this duration and exit abruptly (e.g. 5s)")
	flag.Parse()

	log.Printf("connecting to %s", *url)

	conn, _, err := websocket.DefaultDialer.Dial(*url, nil)
	if err != nil {
		log.Fatalf("dial error: %v", err)
	}
	defer conn.Close()

	log.Printf("connected — will close abruptly (RST, no WebSocket close handshake)")

	conn.SetPingHandler(func(data string) error {
		log.Printf("[ping] received: %q", data)
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})
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

	var sendAfterC <-chan time.Time
	if *sendAfter > 0 {
		sendAfterC = time.After(*sendAfter)
		log.Printf("will send data after %s and close abruptly on response", *sendAfter)
	}

	for {
		select {
		case <-done:
			return

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
				log.Printf("[send] response received, closing abruptly")
			case <-time.After(10 * time.Second):
				log.Printf("[send] timeout waiting for response")
			case <-done:
				return
			}
			abruptClose(conn)
			return

		case <-interrupt:
			log.Printf("interrupted, closing abruptly (RST)")
			abruptClose(conn)
			return
		}
	}
}
