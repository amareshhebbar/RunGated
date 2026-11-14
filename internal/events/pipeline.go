package events

import (
	"context"
	"log/slog"
	"sync"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/store"
)

type Pipeline struct {
	Bus     Bus
	Store   store.Store
	Auditor *Auditor
	Sinks   []Sink
	wg      sync.WaitGroup
	cancel  context.CancelFunc
}

func (p *Pipeline) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		err := p.Bus.Consume(ctx, "rungated-writer", func(evs []core.Event) error {
			if err := p.Store.AppendEvents(context.Background(), evs); err != nil {
				return err
			}
			for _, e := range evs {
				if _, err := p.Auditor.Append(context.Background(), e); err != nil {
					slog.Error("audit append failed", "event", e.ID, "err", err)
					return err
				}
			}
			for _, s := range p.Sinks {
				if err := s.Write(context.Background(), evs); err != nil {
					slog.Warn("sink write failed", "sink", s.Name(), "err", err)
				}
			}
			return nil
		})
		if err != nil {
			slog.Error("event consumer stopped", "err", err)
		}
	}()
}

func (p *Pipeline) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}
