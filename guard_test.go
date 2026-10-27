package main

import (
	"errors"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
)

// unmappableStore stands in for an mmm layer: once closed, any touch is a
// "SIGSEGV" (a panic recorded in faults). Operations sleep so they overlap
// with the close.
type unmappableStore struct {
	unmapped atomic.Bool
	faults   atomic.Int64
	events   []nostr.Event
}

func (s *unmappableStore) touch() {
	if s.unmapped.Load() {
		s.faults.Add(1)
		panic("touched unmapped store")
	}
}

func (s *unmappableStore) op() {
	s.touch()
	time.Sleep(200 * time.Microsecond)
	s.touch()
}

func (s *unmappableStore) Init() error { return nil }
func (s *unmappableStore) Close()      { s.unmapped.Store(true) }
func (s *unmappableStore) QueryEvents(nostr.Filter, int) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		for _, evt := range s.events {
			s.op()
			if !yield(evt) {
				return
			}
			s.touch()
		}
	}
}
func (s *unmappableStore) DeleteEvent(nostr.ID) error               { s.op(); return nil }
func (s *unmappableStore) SaveEvent(nostr.Event) error              { s.op(); return nil }
func (s *unmappableStore) CountEvents(nostr.Filter) (uint32, error) { s.op(); return 0, nil }
func (s *unmappableStore) ReplaceEvent(nostr.Event) ([]nostr.Event, error) {
	s.op()
	return nil, nil
}

func newUnmappable(n int) *unmappableStore {
	s := &unmappableStore{}
	for i := 0; i < n; i++ {
		s.events = append(s.events, nostr.Event{Kind: 1, Content: fmt.Sprint(i)})
	}
	return s
}

// TestGuardedStoreCloseUnderLoad hammers two guarded layers sharing one gate
// (like "main" and "blossom") with saves, replaces, deletes, counts and full
// query iterations (including the nested delete-inside-query pattern of
// wipe.go) while the gate is closed and the layers are "unmapped". No
// operation may touch a layer after it is unmapped.
func TestGuardedStoreCloseUnderLoad(t *testing.T) {
	gate := &storeGate{}
	inners := []*unmappableStore{newUnmappable(20), newUnmappable(20)}
	stores := []eventstore.Store{gate.guard(inners[0]), gate.guard(inners[1])}

	var (
		wg        sync.WaitGroup
		started   sync.WaitGroup
		panics    atomic.Int64
		opsBefore atomic.Int64
	)
	worker := func(s eventstore.Store, kind int) {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panics.Add(1)
			}
		}()
		first := true
		for {
			var err error
			switch kind {
			case 0:
				err = s.SaveEvent(nostr.Event{Kind: 1})
			case 1:
				_, err = s.ReplaceEvent(nostr.Event{Kind: 0})
			case 2:
				err = s.DeleteEvent(nostr.ID{})
			case 3:
				_, err = s.CountEvents(nostr.Filter{})
			case 4:
				n := 0
				for range s.QueryEvents(nostr.Filter{}, 100) {
					n++
				}
				if n == 0 {
					err = ErrStoreClosed
				}
			case 5:
				n := 0
				for evt := range s.QueryEvents(nostr.Filter{}, 100) {
					n++
					if derr := s.DeleteEvent(evt.ID); derr != nil && !errors.Is(derr, ErrStoreClosed) {
						t.Errorf("nested delete: %v", derr)
					}
				}
				if n == 0 {
					err = ErrStoreClosed
				}
			}
			if first {
				started.Done()
				first = false
			}
			if err != nil {
				if !errors.Is(err, ErrStoreClosed) {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			opsBefore.Add(1)
		}
	}
	for _, s := range stores {
		for kind := 0; kind < 6; kind++ {
			for i := 0; i < 4; i++ {
				wg.Add(1)
				started.Add(1)
				go worker(s, kind)
			}
		}
	}
	started.Wait()
	time.Sleep(20 * time.Millisecond)

	if err := gate.Close(5 * time.Second); err != nil {
		t.Fatalf("gate close: %v", err)
	}
	for _, inner := range inners {
		inner.Close() // what mmmm.Close does to every layer
	}
	wg.Wait()

	if n := panics.Load(); n != 0 {
		t.Fatalf("%d operations touched the store after it was unmapped", n)
	}
	for i, inner := range inners {
		if n := inner.faults.Load(); n != 0 {
			t.Fatalf("layer %d: %d faults", i, n)
		}
	}
	if opsBefore.Load() == 0 {
		t.Fatal("no operation ran before close; test did not overlap")
	}

	// every operation after close fails without touching the layer
	for _, s := range stores {
		if err := s.SaveEvent(nostr.Event{}); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("SaveEvent after close: %v", err)
		}
		if _, err := s.ReplaceEvent(nostr.Event{}); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("ReplaceEvent after close: %v", err)
		}
		if err := s.DeleteEvent(nostr.ID{}); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("DeleteEvent after close: %v", err)
		}
		if _, err := s.CountEvents(nostr.Filter{}); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("CountEvents after close: %v", err)
		}
		for range s.QueryEvents(nostr.Filter{}, 10) {
			t.Error("QueryEvents yielded after close")
		}
	}
}

// TestGuardedStoreCloseWaitsForIteration checks that Close blocks while a
// QueryEvents loop is mid-iteration, that a nested op started during the close
// fails instead of deadlocking, and that Close returns once the loop ends.
func TestGuardedStoreCloseWaitsForIteration(t *testing.T) {
	gate := &storeGate{}
	inner := newUnmappable(3)
	s := gate.guard(inner)

	inLoop := make(chan struct{})
	release := make(chan struct{})
	nestedErr := make(chan error, 1)
	loopDone := make(chan int)
	go func() {
		n := 0
		for range s.QueryEvents(nostr.Filter{}, 10) {
			n++
			if n == 1 {
				close(inLoop)
				<-release
				nestedErr <- s.SaveEvent(nostr.Event{})
			}
		}
		loopDone <- n
	}()
	<-inLoop

	closed := make(chan error, 1)
	go func() { closed <- gate.Close(5 * time.Second) }()

	select {
	case err := <-closed:
		t.Fatalf("Close returned during iteration: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	if err := <-nestedErr; !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("nested op during close: got %v, want ErrStoreClosed", err)
	}
	// Once Close began, the query stops yielding: an unbounded query must not
	// outlast the close timeout.
	if n := <-loopDone; n != 1 {
		t.Fatalf("iteration kept yielding after Close began: %d events, want 1", n)
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	inner.Close()
}

// TestGuardedStoreAbandonedIterationReleases covers a loop that breaks early
// and one whose body panics: both must release the gate.
func TestGuardedStoreAbandonedIterationReleases(t *testing.T) {
	gate := &storeGate{}
	s := gate.guard(newUnmappable(5))

	for range s.QueryEvents(nostr.Filter{}, 10) {
		break
	}
	func() {
		defer func() { recover() }()
		for range s.QueryEvents(nostr.Filter{}, 10) {
			panic("boom")
		}
	}()
	// a query that is built but never ranged holds nothing
	_ = s.QueryEvents(nostr.Filter{}, 10)

	if err := gate.Close(100 * time.Millisecond); err != nil {
		t.Fatalf("gate still held: %v", err)
	}
}

// TestStoreGateCloseTimeout: when an operation outlives the timeout, Close
// reports it so the caller leaves mmm open instead of unmapping under it.
func TestStoreGateCloseTimeout(t *testing.T) {
	gate := &storeGate{}
	s := gate.guard(newUnmappable(2))

	inLoop := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range s.QueryEvents(nostr.Filter{}, 10) {
			close(inLoop)
			<-release
			break
		}
	}()
	<-inLoop
	if err := gate.Close(20 * time.Millisecond); err == nil {
		t.Fatal("Close succeeded with an operation in flight")
	}
	close(release)
	<-done
	if err := gate.Close(time.Second); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestCloseStoreRealMMM runs the guard against a real mmm store. It needs
// LMDB (on macOS, free POSIX semaphores) and is skipped when the store cannot
// be opened.
func TestCloseStoreRealMMM(t *testing.T) {
	mm, gate, s, err := initStore(t.TempDir())
	if err != nil {
		t.Skipf("cannot open mmm here: %v", err)
	}
	blossomLayer, err := mm.EnsureLayer("blossom")
	if err != nil {
		mm.Close()
		t.Fatalf("blossom layer: %v", err)
	}
	stores := []eventstore.Store{s, gate.guard(blossomLayer)}

	sk := nostr.Generate()
	var wg sync.WaitGroup
	var saved atomic.Int64
	for i, st := range stores {
		for w := 0; w < 4; w++ {
			wg.Add(2)
			go func(st eventstore.Store, seed int) {
				defer wg.Done()
				for j := 0; ; j++ {
					evt := nostr.Event{Kind: 1, CreatedAt: nostr.Now(), Content: fmt.Sprint(seed, "-", j)}
					evt.Sign(sk)
					if err := st.SaveEvent(evt); err != nil {
						if !errors.Is(err, ErrStoreClosed) {
							t.Errorf("save: %v", err)
						}
						return
					}
					saved.Add(1)
				}
			}(st, i*10+w)
			go func(st eventstore.Store) {
				defer wg.Done()
				for {
					n := 0
					for evt := range st.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{1}}, 200) {
						_ = evt.Content
						n++
					}
					if _, err := st.CountEvents(nostr.Filter{Kinds: []nostr.Kind{1}}); errors.Is(err, ErrStoreClosed) {
						return
					}
				}
			}(st)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if err := closeStore(mm, gate, 5*time.Second); err != nil {
		t.Fatalf("closeStore: %v", err)
	}
	wg.Wait()
	if saved.Load() == 0 {
		t.Fatal("no event was saved before close")
	}
	if err := s.SaveEvent(nostr.Event{}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("save after close: %v", err)
	}
}
