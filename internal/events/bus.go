package events

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
)

type Bus interface {
	Publish(e core.Event)
	Subscribe(buf int) (<-chan core.Event, func())
	Consume(ctx context.Context, name string, handler func([]core.Event) error) error
	Close() error
}

type fanout struct {
	mu   sync.RWMutex
	subs map[int]chan core.Event
	next int
}

func newFanout() *fanout { return &fanout{subs: map[int]chan core.Event{}} }

func (f *fanout) publish(e core.Event) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, ch := range f.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (f *fanout) subscribe(buf int) (<-chan core.Event, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.next
	f.next++
	ch := make(chan core.Event, buf)
	f.subs[id] = ch
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			f.mu.Lock()
			delete(f.subs, id)
			close(ch)
			f.mu.Unlock()
		})
	}
}

type Local struct {
	live    *fanout
	queue   chan core.Event
	dropped uint64
	mu      sync.Mutex
}

func NewLocal(queue int) *Local {
	if queue <= 0 {
		queue = 65536
	}
	return &Local{live: newFanout(), queue: make(chan core.Event, queue)}
}

func (l *Local) Publish(e core.Event) {
	l.live.publish(e)
	select {
	case l.queue <- e:
	default:
		l.mu.Lock()
		l.dropped++
		l.mu.Unlock()
		slog.Warn("event queue full, dropping event from persistence path", "id", e.ID)
	}
}

func (l *Local) Subscribe(buf int) (<-chan core.Event, func()) { return l.live.subscribe(buf) }

func (l *Local) Consume(ctx context.Context, name string, handler func([]core.Event) error) error {
	batch := make([]core.Event, 0, 256)
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		for attempt := 0; attempt < 5; attempt++ {
			if err := handler(batch); err != nil {
				slog.Error("event handler failed", "consumer", name, "err", err, "attempt", attempt)
				time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
				continue
			}
			break
		}
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case e := <-l.queue:
					batch = append(batch, e)
				default:
					flush()
					return nil
				}
			}
		case e := <-l.queue:
			batch = append(batch, e)
			if len(batch) >= 256 {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

func (l *Local) Close() error { return nil }
