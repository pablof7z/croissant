package main

import (
	"io"
	"net"
	"net/http"
	"sync"
)

// hijackedConns tracks connections that http.Server handed off via Hijack
// (khatru websockets). http.Server.Shutdown neither waits for nor closes them,
// so without this their handlers keep running and clients never see the
// relay go away.
//
// The listener wrapper lets a connection drop itself from the set when it is
// closed, since http.Server reports no StateClosed for hijacked connections.
type hijackedConns struct {
	mu    sync.Mutex
	conns map[*trackedConn]struct{}
}

type trackedConn struct {
	net.Conn
	owner     *hijackedConns
	closeOnce sync.Once
	closeErr  error
}

func (c *trackedConn) Close() error {
	c.closeOnce.Do(func() {
		c.owner.mu.Lock()
		delete(c.owner.conns, c)
		c.owner.mu.Unlock()
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}

// ReadFrom keeps net/http's sendfile path (it type-asserts io.ReaderFrom on the
// connection) working through the wrapper.
func (c *trackedConn) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := c.Conn.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(c.Conn, r)
}

type trackingListener struct {
	net.Listener
	owner *hijackedConns
}

func (l trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &trackedConn{Conn: c, owner: l.owner}, nil
}

// Listener wraps ln so ConnState can recognize and track its connections.
func (h *hijackedConns) Listener(ln net.Listener) net.Listener {
	return trackingListener{Listener: ln, owner: h}
}

// ConnState is meant for http.Server.ConnState.
func (h *hijackedConns) ConnState(c net.Conn, state http.ConnState) {
	if state != http.StateHijacked {
		return
	}
	tc, ok := c.(*trackedConn)
	if !ok {
		return
	}
	h.mu.Lock()
	if h.conns == nil {
		h.conns = make(map[*trackedConn]struct{})
	}
	h.conns[tc] = struct{}{}
	h.mu.Unlock()
}

// CloseAll closes every hijacked connection still open and returns how many
// it closed.
func (h *hijackedConns) CloseAll() int {
	h.mu.Lock()
	conns := make([]*trackedConn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
	return len(conns)
}

// Len returns how many hijacked connections are currently tracked.
func (h *hijackedConns) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}
