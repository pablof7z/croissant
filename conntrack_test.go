package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"fiatjaf.com/nostr/khatru"
	"github.com/fasthttp/websocket"
)

// TestShutdownClosesHijackedWebsockets: http.Server.Shutdown leaves hijacked
// khatru websockets open; shutdownServer must close them so the relay's
// connection handler ends and the client sees the connection drop.
func TestShutdownClosesHijackedWebsockets(t *testing.T) {
	relay := khatru.NewRelay()
	disconnected := make(chan struct{}, 4)
	relay.OnDisconnect = func(ctx context.Context) { disconnected <- struct{}{} }

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hijacked := &hijackedConns{}
	server := &http.Server{Handler: relay, ConnState: hijacked.ConnState}
	go server.Serve(hijacked.Listener(ln))

	const clients = 3
	conns := make([]*websocket.Conn, 0, clients)
	for i := 0; i < clients; i++ {
		c, _, err := websocket.DefaultDialer.Dial("ws://"+ln.Addr().String(), nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer c.Close()
		conns = append(conns, c)
	}
	// a client that disconnects on its own must drop out of the set
	extra, _, err := websocket.DefaultDialer.Dial("ws://"+ln.Addr().String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitFor(t, func() bool { return hijacked.Len() == clients+1 }, "all connections hijacked")
	extra.Close()
	<-disconnected
	waitFor(t, func() bool { return hijacked.Len() == clients }, "closed connection untracked")

	if err := shutdownServer(server, hijacked, time.Second); err != nil {
		t.Fatalf("shutdownServer: %v", err)
	}

	for i := 0; i < clients; i++ {
		select {
		case <-disconnected:
		case <-time.After(2 * time.Second):
			t.Fatalf("relay handler still running for %d connection(s)", clients-i)
		}
	}
	for _, c := range conns {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err == nil {
			t.Fatal("client connection still open")
		} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatal("client connection still open (read timed out)")
		}
	}
	if n := hijacked.Len(); n != 0 {
		t.Fatalf("%d connections still tracked", n)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
