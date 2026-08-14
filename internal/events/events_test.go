package events

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"
)

// recordingSink captures every batch it is handed.
type recordingSink struct {
	mu      sync.Mutex
	events  []Event
	err     error
	release chan struct{} // when non-nil, InsertEvents blocks until it closes
}

func (s *recordingSink) InsertEvents(ctx context.Context, batch []Event) error {
	if s.release != nil {
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, batch...)
	return s.err
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func TestLevelForDerivesSeverityFromType(t *testing.T) {
	cases := map[Type]Level{
		ScrapeFailed:        LevelError,
		AIGenerationFailed:  LevelError,
		PostFailed:          LevelError,
		JobSkipped:          LevelWarn,
		JobCancelled:        LevelWarn,
		ProductRemoved:      LevelWarn,
		ScrapeSuccess:       LevelSuccess,
		AIGenerationSuccess: LevelSuccess,
		PostSuccess:         LevelSuccess,
		JobCompleted:        LevelSuccess,
		ScrapeStarted:       LevelInfo,
		ProductQueued:       LevelInfo,
		JobCreated:          LevelInfo,
	}

	for typ, want := range cases {
		if got := levelFor(typ); got != want {
			t.Errorf("levelFor(%s) = %s, want %s", typ, got, want)
		}
	}
}

func TestEmitFlushesToSink(t *testing.T) {
	sink := &recordingSink{}
	logger := New(sink)

	logger.Emit(Event{Type: PostSuccess, Source: SourceFacebook, TraceID: "t1"})
	logger.Emit(Event{Type: ScrapeFailed, Source: SourceAmazon, TraceID: "t1"})
	logger.Close()

	if got := sink.count(); got != 2 {
		t.Fatalf("sink received %d events, want 2", got)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.events[0].Level != LevelSuccess {
		t.Errorf("level not populated on emit: got %q", sink.events[0].Level)
	}
	if sink.events[0].CreatedAt.IsZero() {
		t.Error("CreatedAt not populated on emit")
	}
}

func TestEmitPreservesExplicitLevel(t *testing.T) {
	sink := &recordingSink{}
	logger := New(sink)

	// An explicitly set level must survive - a caller downgrading a failure to
	// a warning (say, a retryable one) shouldn't be overwritten by the suffix.
	logger.Emit(Event{Type: PostFailed, Level: LevelWarn, TraceID: "t1"})
	logger.Close()

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.events[0].Level != LevelWarn {
		t.Errorf("explicit level overwritten: got %q, want %q", sink.events[0].Level, LevelWarn)
	}
}

func TestEmitDropsWhenBufferFullRatherThanBlocking(t *testing.T) {
	// Hold the sink open so the writer goroutine is stuck mid-flush and the
	// buffer has no way to drain.
	sink := &recordingSink{release: make(chan struct{})}
	logger := New(sink)

	// Comfortably more than bufferSize; without the drop path this would block
	// forever and the test would time out.
	const emitted = bufferSize * 3
	done := make(chan struct{})
	go func() {
		for i := 0; i < emitted; i++ {
			logger.Emit(Event{Type: ScrapeStarted, TraceID: "flood"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Emit blocked when the buffer was full; it must drop instead")
	}

	if logger.Dropped() == 0 {
		t.Error("expected dropped events to be counted")
	}

	close(sink.release)
	logger.Close()
}

func TestCloseDrainsBufferedEvents(t *testing.T) {
	sink := &recordingSink{}
	logger := New(sink)

	// Fewer than batchSize and well inside the flush interval, so these only
	// reach the sink if Close drains rather than just stopping.
	for i := 0; i < 5; i++ {
		logger.Emit(Event{Type: ProductQueued, TraceID: "t1"})
	}
	logger.Close()

	if got := sink.count(); got != 5 {
		t.Errorf("Close lost buffered events: sink got %d, want 5", got)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	logger := New(&recordingSink{})
	logger.Close()
	logger.Close() // must not panic on a second close
}

func TestSinkErrorDoesNotPropagate(t *testing.T) {
	sink := &recordingSink{err: errors.New("database unreachable")}
	logger := New(sink)

	// The point of the exercise: a failing sink must not panic or block the
	// caller. Emit has no error return by design.
	logger.Emit(Event{Type: PostSuccess, TraceID: "t1"})
	logger.Close()
}

func TestNilSinkIsNoOp(t *testing.T) {
	logger := New(nil)
	logger.Emit(Event{Type: PostSuccess, TraceID: "t1"})
	if logger.Dropped() != 0 {
		t.Error("no-op logger should not count drops")
	}
	logger.Close()
}

func TestNilLoggerIsSafe(t *testing.T) {
	// cmd/cli constructs no logger at all in JSON-fallback mode, so the nil
	// receiver has to be safe rather than requiring a guard at every call site.
	var logger *Logger
	logger.Emit(Event{Type: PostSuccess})
	logger.Close()
	if logger.Dropped() != 0 {
		t.Error("nil logger should report zero drops")
	}
}

func TestNewTraceIDIsAValidV4(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := NewTraceID()
		if !pattern.MatchString(id) {
			t.Fatalf("trace id %q is not a v4 UUID", id)
		}
		if seen[id] {
			t.Fatalf("trace id %q was generated twice", id)
		}
		seen[id] = true
	}
}
