package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type wsHandler struct {
	idleTimeout time.Duration
	noPong      bool
}

func (h *wsHandler) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	defer conn.Close()

	remoteAddr := r.RemoteAddr
	log.Printf("[%s] connected", remoteAddr)

	resetDeadline := func() {
		if h.idleTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(h.idleTimeout))
		}
	}
	resetDeadline()

	if h.noPong {
		conn.SetPingHandler(func(data string) error {
			log.Printf("[%s] received ping (pong suppressed)", remoteAddr)
			resetDeadline()
			return nil
		})
	} else {
		conn.SetPingHandler(func(data string) error {
			log.Printf("[%s] received ping, sending pong", remoteAddr)
			resetDeadline()
			return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(10*time.Second))
		})
	}

	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[%s] error: %v", remoteAddr, err)
			} else {
				log.Printf("[%s] disconnected", remoteAddr)
			}
			return
		}
		resetDeadline()
		log.Printf("[%s] received message type=%d: %s", remoteAddr, msgType, string(msg))
		if err := conn.WriteMessage(msgType, msg); err != nil {
			log.Printf("[%s] write error: %v", remoteAddr, err)
			return
		}
	}
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	idleTimeout := flag.Duration("idle-timeout", 0, "server-side idle timeout per connection (0 = disabled, e.g. 60s)")
	noPong := flag.Bool("no-pong", false, "do not send pong in response to client ping frames")
	flag.Parse()

	h := &wsHandler{idleTimeout: *idleTimeout, noPong: *noPong}
	http.HandleFunc("/ws", h.handleWS)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	if *idleTimeout > 0 {
		log.Printf("server listening on %s (idle-timeout=%s, no-pong=%v)", *addr, *idleTimeout, *noPong)
	} else {
		log.Printf("server listening on %s (idle-timeout=disabled, no-pong=%v)", *addr, *noPong)
	}
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}
