// Package events provides the append-only pipeline event log.
//
// Every stage of the auto-post pipeline - queueing, scraping, AI enrichment,
// publishing, and job lifecycle - emits an Event here. Events sharing a
// TraceID belong to one pipeline run, so the full story of a single post is a
// single indexed query rather than a hunt through stdout.
//
// The one hard rule: emitting an event must never be able to fail a publish.
// Emit never blocks and never returns an error; when the buffer is full events
// are dropped and counted rather than applying backpressure to the pipeline.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Type identifies what happened. The suffix determines the Level, so new types
// should keep the _STARTED / _SUCCESS / _FAILED / _SKIPPED / _CANCELLED shape.
type Type string

const (
	ProductQueued  Type = "PRODUCT_QUEUED"
	ProductRemoved Type = "PRODUCT_REMOVED"
	JobCreated     Type = "JOB_CREATED"
	JobCancelled   Type = "JOB_CANCELLED"

	ScrapeStarted Type = "SCRAPE_STARTED"
	ScrapeSuccess Type = "SCRAPE_SUCCESS"
	ScrapeFailed  Type = "SCRAPE_FAILED"

	AIGenerationStarted Type = "AI_GENERATION_STARTED"
	AIGenerationSuccess Type = "AI_GENERATION_SUCCESS"
	AIGenerationFailed  Type = "AI_GENERATION_FAILED"

	PostStarted Type = "POST_STARTED"
	PostSuccess Type = "POST_SUCCESS"
	PostFailed  Type = "POST_FAILED"

	JobStarted   Type = "JOB_STARTED"
	JobCompleted Type = "JOB_COMPLETED"
	JobSkipped   Type = "JOB_SKIPPED"

	// One discovery run, across the whole query matrix.
	DiscoveryStarted Type = "DISCOVERY_STARTED"
	DiscoverySuccess Type = "DISCOVERY_SUCCESS"
	DiscoveryFailed  Type = "DISCOVERY_FAILED"

	// Individual deals moving through the pipeline.
	DealDiscovered Type = "DEAL_DISCOVERED"
	DealUpdated    Type = "DEAL_UPDATED"
	DealScored     Type = "DEAL_SCORED"
	DealQueued     Type = "DEAL_QUEUED"
	DealPosted     Type = "DEAL_POSTED"
	DealExpired    Type = "DEAL_EXPIRED"
)

// Level is the severity band the Activity Log filters on.
type Level string

const (
	LevelInfo    Level = "INFO"
	LevelSuccess Level = "SUCC"
	LevelWarn    Level = "WARN"
	LevelError   Level = "ERR"
)

// Source names the subsystem that produced the event.
const (
	SourceAmazon   = "amazon"
	SourceOllama   = "ollama"
	SourceGemini   = "gemini"
	SourceFacebook = "facebook"
	SourceQueue    = "queue"
	SourceWorker   = "worker"
	// SourceDiscovery covers deal discovery regardless of which provider
	// served it; which one did is recorded in the event metadata.
	SourceDiscovery = "discovery"
)

// levelFor maps a Type to its severity. Types that report a terminal failure
// are errors; a skip is a warning (it's expected but worth surfacing); a
// completed action is a success; everything else is informational.
func levelFor(t Type) Level {
	switch t {
	case ScrapeFailed, AIGenerationFailed, PostFailed, DiscoveryFailed:
		return LevelError
	case JobSkipped, JobCancelled, ProductRemoved, DealExpired:
		return LevelWarn
	case ScrapeSuccess, AIGenerationSuccess, PostSuccess, JobCompleted,
		DiscoverySuccess, DealPosted:
		return LevelSuccess
	default:
		return LevelInfo
	}
}

// Event is one record in the log.
type Event struct {
	Type       Type
	Level      Level
	Source     string
	TraceID    string
	Account    string
	ProductURL string
	JobID      *int
	JobItemID  *int
	Message    string
	Duration   time.Duration
	Metadata   map[string]any
	CreatedAt  time.Time
}

// Sink persists a batch of events. *db.Pool satisfies this; keeping it an
// interface is what lets this package stay independent of internal/db.
type Sink interface {
	InsertEvents(ctx context.Context, batch []Event) error
}

const (
	bufferSize    = 512
	batchSize     = 50
	flushInterval = time.Second
)

// Logger buffers events and writes them in batches on a single goroutine.
// The zero value is not usable; construct one with New.
type Logger struct {
	sink    Sink
	ch      chan Event
	done    chan struct{}
	wg      sync.WaitGroup
	dropped atomic.Uint64
	closeMu sync.Mutex
	closed  bool
}

// New returns a Logger writing to sink. A nil sink yields a no-op Logger, so
// callers running without a database (the JSON fallback mode) need no branch.
func New(sink Sink) *Logger {
	if sink == nil {
		return &Logger{}
	}

	l := &Logger{
		sink: sink,
		ch:   make(chan Event, bufferSize),
		done: make(chan struct{}),
	}
	l.wg.Add(1)
	go l.run()
	return l
}

// Emit records an event. It never blocks and never fails: if the buffer is
// full the event is dropped and counted, because losing a log line is always
// preferable to stalling the publish that produced it.
func (l *Logger) Emit(e Event) {
	if l == nil || l.ch == nil {
		return
	}

	if e.Level == "" {
		e.Level = levelFor(e.Type)
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}

	select {
	case l.ch <- e:
	default:
		l.dropped.Add(1)
	}
}

// Dropped reports how many events have been discarded due to a full buffer.
// Surfaced on /health so sustained loss is visible rather than silent.
func (l *Logger) Dropped() uint64 {
	if l == nil {
		return 0
	}
	return l.dropped.Load()
}

// Close stops the writer and flushes whatever is still buffered. It is safe to
// call more than once.
func (l *Logger) Close() {
	if l == nil || l.ch == nil {
		return
	}

	l.closeMu.Lock()
	if l.closed {
		l.closeMu.Unlock()
		return
	}
	l.closed = true
	close(l.done)
	l.closeMu.Unlock()

	l.wg.Wait()
}

// run drains the channel, writing whenever the batch fills or the flush
// interval elapses, whichever comes first.
func (l *Logger) run() {
	defer l.wg.Done()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]Event, 0, batchSize)

	for {
		select {
		case e := <-l.ch:
			batch = append(batch, e)
			if len(batch) >= batchSize {
				batch = l.flush(batch)
			}

		case <-ticker.C:
			batch = l.flush(batch)

		case <-l.done:
			// Drain whatever is still queued before exiting so a clean
			// shutdown doesn't lose the tail of the log.
			for {
				select {
				case e := <-l.ch:
					batch = append(batch, e)
					if len(batch) >= batchSize {
						batch = l.flush(batch)
					}
					continue
				default:
				}
				break
			}
			l.flush(batch)
			return
		}
	}
}

// flush writes the batch and returns it truncated for reuse. A write failure
// is logged and the batch discarded - retrying risks unbounded growth, and
// this is telemetry, not the pipeline's own state.
func (l *Logger) flush(batch []Event) []Event {
	if len(batch) == 0 {
		return batch
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := l.sink.InsertEvents(ctx, batch); err != nil {
		log.Printf("[WARN] Dropping %d event(s), insert failed: %v", len(batch), err)
	}

	return batch[:0]
}

// NewTraceID returns a random RFC-4122 v4 identifier used to tie together
// every event produced by one pipeline run.
func NewTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is not recoverable here, and a trace ID is not
		// worth panicking over - fall back to a timestamp-derived value.
		return "trace-" + time.Now().UTC().Format("20060102150405.000000000")
	}

	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
