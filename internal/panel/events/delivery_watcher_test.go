package events

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeDeliveryReader is a minimal contract.DeliveryReader stand-in:
// WatchDeliveryView blocks (like the real daemon's long-poll) until either
// advance() is called for that orchestration id or ctx is done, so
// DeliveryWatcher's own watchOne loop is exercised the same way it would be
// against a real daemon rather than hot-looping against an instantly
// returning fake.
type fakeDeliveryReader struct {
	mu         sync.Mutex
	list       []*protocol.DeliveryOrchestration
	seq        map[string]int
	notify     map[string]chan struct{}
	watchCalls atomic.Int64
}

func newFakeDeliveryReader() *fakeDeliveryReader {
	return &fakeDeliveryReader{seq: map[string]int{}, notify: map[string]chan struct{}{}}
}

func (f *fakeDeliveryReader) addOrchestration(id string, status protocol.DeliveryOrchestrationStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list = append(f.list, &protocol.DeliveryOrchestration{Id: id, Status: status})
	if _, ok := f.notify[id]; !ok {
		f.notify[id] = make(chan struct{})
	}
}

// advance bumps id's LatestSeq to newSeq and wakes any blocked
// WatchDeliveryView call for it.
func (f *fakeDeliveryReader) advance(id string, newSeq int) {
	f.mu.Lock()
	f.seq[id] = newSeq
	ch := f.notify[id]
	f.notify[id] = make(chan struct{})
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (f *fakeDeliveryReader) snapshot(id string) *delivery.DeliveryView {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &delivery.DeliveryView{LatestSeq: f.seq[id], NewlyRunnableLaneIDs: []string{}}
}

func (f *fakeDeliveryReader) ListDeliveries(ctx context.Context) ([]*protocol.DeliveryOrchestration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*protocol.DeliveryOrchestration, len(f.list))
	copy(out, f.list)
	return out, nil
}

func (f *fakeDeliveryReader) GetDeliveryView(ctx context.Context, orchestrationID string, sinceSeq int) (*delivery.DeliveryView, error) {
	return f.snapshot(orchestrationID), nil
}

func (f *fakeDeliveryReader) WatchDeliveryView(ctx context.Context, orchestrationID string, sinceSeq, waitSeconds int) (*delivery.DeliveryView, error) {
	f.watchCalls.Add(1)
	f.mu.Lock()
	ch := f.notify[orchestrationID]
	cur := f.seq[orchestrationID]
	f.mu.Unlock()
	if cur != sinceSeq {
		return f.snapshot(orchestrationID), nil
	}
	select {
	case <-ch:
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
	}
	return f.snapshot(orchestrationID), nil
}

func (f *fakeDeliveryReader) AnswerDeliveryQuestion(ctx context.Context, orchestrationID string, in daemon.AnswerDeliveryQuestionRequest) (*delivery.DeliveryView, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDeliveryReader) ApproveProjectDelivery(ctx context.Context, orchestrationID string, in daemon.ApproveProjectDeliveryRequest) (*delivery.DeliveryView, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDeliveryReader) CancelDelivery(ctx context.Context, orchestrationID string, in daemon.CancelDeliveryRequest) (*delivery.DeliveryView, error) {
	return nil, errors.New("not implemented")
}

func TestDeliveryWatcherPublishesOnLatestSeqAdvance(t *testing.T) {
	reader := newFakeDeliveryReader()
	reader.addOrchestration("orc-1", protocol.DeliveryOrchestrationStatusActive)

	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	watcher := &DeliveryWatcher{Hub: hub, Reader: reader}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watcher.Run(ctx)

	// Give discover()'s first pass time to spawn watchOne before advancing -
	// not required for correctness (WatchDeliveryView's own cur!=sinceSeq
	// check handles either ordering), just keeps this test's intent obvious.
	time.Sleep(20 * time.Millisecond)
	reader.advance("orc-1", 5)

	select {
	case evt := <-ch:
		if evt.Type != protocol.PanelEventTypeDeliveryUpdated {
			t.Fatalf("Type = %q, want delivery.updated", evt.Type)
		}
		if evt.EntityId == nil || *evt.EntityId != "orc-1" {
			t.Fatalf("EntityId = %v, want orc-1", evt.EntityId)
		}
		if got, _ := evt.Payload["latest_seq"].(int); got != 5 {
			t.Fatalf("payload latest_seq = %v, want 5", evt.Payload["latest_seq"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delivery.updated")
	}
}

func TestDeliveryWatcherSkipsTerminalOrchestrations(t *testing.T) {
	reader := newFakeDeliveryReader()
	reader.addOrchestration("orc-done", protocol.DeliveryOrchestrationStatusCompleted)

	watcher := &DeliveryWatcher{Hub: NewHub(), Reader: reader}
	ctx, cancel := context.WithCancel(context.Background())
	go watcher.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	if calls := reader.watchCalls.Load(); calls != 0 {
		t.Fatalf("WatchDeliveryView called %d times for a completed orchestration, want 0", calls)
	}
}

func TestDeliveryWatcherNilReaderIsNoop(t *testing.T) {
	watcher := &DeliveryWatcher{Hub: NewHub(), Reader: nil}
	done := make(chan struct{})
	go func() {
		watcher.Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run with a nil Reader did not return promptly")
	}
}
