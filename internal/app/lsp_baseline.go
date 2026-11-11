package app

// lspBaselineUpdater applies the LSP port's trust-gated config baseline from a single
// serialized worker. Settings reloads and live trust changes race (a trust callback can
// observe C1 while a reload has just installed C2); the sequence number plus one worker
// ensures the newest state is what is finally applied and an older snapshot can never
// overwrite a newer baseline. The callers only update state and signal, so a trust dialog
// action on the UI goroutine never blocks on SetConfig (which can wait behind an explicit
// language-server start).

import (
	"context"
	"sync"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/trust"
)

type lspConfigSetter interface {
	SetConfig(config.Config)
}

type lspBaselineUpdater struct {
	setter   lspConfigSetter
	liveGate bool

	mu          sync.Mutex
	cfg         config.Config
	trust       trust.State
	seq         uint64
	applied     uint64
	wake        chan struct{}
	appliedWake chan struct{}
	ctx         context.Context
}

// newLSPBaselineUpdater starts the serialized baseline worker. It returns nil when the LSP
// port has no SetConfig seam.
func newLSPBaselineUpdater(ctx context.Context, lsp any, cfg config.Config, state trust.State) *lspBaselineUpdater {
	setter, ok := lsp.(lspConfigSetter)
	if !ok {
		return nil
	}
	_, liveGate := lsp.(interface{ SetAutoStartGate(func() bool) })
	b := &lspBaselineUpdater{
		setter:      setter,
		liveGate:    liveGate,
		cfg:         cfg,
		trust:       state,
		wake:        make(chan struct{}, 1),
		appliedWake: make(chan struct{}),
		ctx:         ctx,
	}
	go b.run(ctx)
	return b
}

func (b *lspBaselineUpdater) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// UpdateConfig installs a reloaded config together with the trust state observed with it.
func (b *lspBaselineUpdater) UpdateConfig(cfg config.Config, state trust.State) {
	b.mu.Lock()
	b.cfg = cfg
	b.trust = state
	b.seq++
	b.mu.Unlock()
	b.signal()
}

// UpdateTrust changes only the live trust state; the latest config is retained, so a stale
// callback that used the old settings snapshot can never push that snapshot back.
func (b *lspBaselineUpdater) UpdateTrust(state trust.State) {
	b.mu.Lock()
	b.trust = state
	b.seq++
	b.mu.Unlock()
	b.signal()
}

// Applied reports the last sequence applied. Seq reports the latest requested sequence; the
// pair is for tests to wait for the worker without polling UI state.
func (b *lspBaselineUpdater) Applied() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.applied
}

func (b *lspBaselineUpdater) Seq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Wait waits for the updates requested before it was called. It runs off the UI
// goroutine, before an LSP open, so the open sees the settled baseline.
func (b *lspBaselineUpdater) Wait() {
	b.mu.Lock()
	target := b.seq
	for b.applied < target {
		wake := b.appliedWake
		b.mu.Unlock()
		select {
		case <-wake:
		case <-b.ctx.Done():
			return
		}
		b.mu.Lock()
	}
	b.mu.Unlock()
}

// run applies the newest snapshot repeatedly until the sequence is stable. A single worker
// serializes applies, so an older snapshot can never land after a newer one.
func (b *lspBaselineUpdater) run(ctx context.Context) {
	var done uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.wake:
		}
		for {
			b.mu.Lock()
			seq := b.seq
			cfg := b.cfg
			state := b.trust
			b.mu.Unlock()
			if seq == done {
				break
			}
			if !b.liveGate {
				cfg = lspConfig(cfg, state)
			}
			b.setter.SetConfig(cfg)
			b.mu.Lock()
			b.applied = seq
			close(b.appliedWake)
			b.appliedWake = make(chan struct{})
			done = seq
			changed := b.seq != seq
			b.mu.Unlock()
			if !changed {
				break
			}
		}
	}
}
