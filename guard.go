package main

import (
	"errors"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
)

// ErrStoreClosed is returned by every guarded store operation that starts after
// the store gate has been closed.
var ErrStoreClosed = errors.New("store is closed")

// storeGate keeps the mmm store from being closed under a running operation.
//
// MultiMmapManager.Close closes every LMDB env and munmaps the event file
// without taking any lock, and websocket handlers (hijacked, so
// http.Server.Shutdown does not wait for them) keep calling the store while
// the process shuts down. Touching the store after Close is a SIGSEGV. Every
// store operation therefore passes through the gate, and Close seals it and
// waits for the operations already inside to leave.
//
// The gate is a counter rather than a sync.RWMutex on purpose: callers nest
// operations (wipe.go deletes inside a QueryEvents loop, reject_event.go and
// group.go query inside query loops), and a nested RLock blocks forever behind
// a pending Lock. Here a nested operation that starts after Close began just
// gets ErrStoreClosed while the outer one runs to completion.
type storeGate struct {
	mu       sync.Mutex
	closed   bool
	inflight int
	drained  chan struct{}
	// sealed mirrors closed for lock-free checks inside query loops, so a
	// long query stops yielding once Close begins instead of outlasting it.
	sealed atomic.Bool
}

func (g *storeGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.inflight++
	return true
}

func (g *storeGate) exit() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.inflight--
	if g.closed && g.inflight == 0 {
		close(g.drained)
	}
}

// Close stops new operations from starting and waits up to timeout for the
// ones in flight to finish. It returns an error if they did not, in which case
// the underlying store must not be closed. Calling Close again is safe.
func (g *storeGate) Close(timeout time.Duration) error {
	g.mu.Lock()
	if !g.closed {
		g.closed = true
		g.sealed.Store(true)
		g.drained = make(chan struct{})
		if g.inflight == 0 {
			close(g.drained)
		}
	}
	drained := g.drained
	g.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-drained:
		return nil
	case <-timer.C:
		g.mu.Lock()
		n := g.inflight
		g.mu.Unlock()
		return fmt.Errorf("%d store operation(s) still running after %s", n, timeout)
	}
}

// guardedStore runs every operation of the wrapped store through a storeGate.
// Init and Close are not forwarded: the MultiMmapManager owns the layers'
// lifecycle and closes them itself, after the gate is closed.
type guardedStore struct {
	gate  *storeGate
	inner eventstore.Store
}

var _ eventstore.Store = (*guardedStore)(nil)

// guard wraps inner so its operations go through g.
func (g *storeGate) guard(inner eventstore.Store) eventstore.Store {
	return &guardedStore{gate: g, inner: inner}
}

func (s *guardedStore) Init() error { return nil }
func (s *guardedStore) Close()      {}

// QueryEvents holds the gate for the whole iteration, from the first pull until
// the loop ends, breaks, or panics. If the store is already closed it yields
// nothing, and once Close begins it stops yielding: an unbounded query
// (negentropy, wipe) must not hold the store open past the close timeout,
// and its results would only go to connections that are being closed.
func (s *guardedStore) QueryEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		if !s.gate.enter() {
			return
		}
		defer s.gate.exit()
		for evt := range s.inner.QueryEvents(filter, maxLimit) {
			if s.gate.sealed.Load() || !yield(evt) {
				return
			}
		}
	}
}

func (s *guardedStore) DeleteEvent(id nostr.ID) error {
	if !s.gate.enter() {
		return ErrStoreClosed
	}
	defer s.gate.exit()
	return s.inner.DeleteEvent(id)
}

func (s *guardedStore) SaveEvent(event nostr.Event) error {
	if !s.gate.enter() {
		return ErrStoreClosed
	}
	defer s.gate.exit()
	return s.inner.SaveEvent(event)
}

func (s *guardedStore) ReplaceEvent(event nostr.Event) ([]nostr.Event, error) {
	if !s.gate.enter() {
		return nil, ErrStoreClosed
	}
	defer s.gate.exit()
	return s.inner.ReplaceEvent(event)
}

func (s *guardedStore) CountEvents(filter nostr.Filter) (uint32, error) {
	if !s.gate.enter() {
		return 0, ErrStoreClosed
	}
	defer s.gate.exit()
	return s.inner.CountEvents(filter)
}
