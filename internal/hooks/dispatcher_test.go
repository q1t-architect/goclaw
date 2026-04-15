package hooks_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

// fakeStore serves a canned hook list for ResolveForEvent and captures any
// Update calls (used to verify circuit-breaker auto-disable).
type fakeStore struct {
	mu       sync.Mutex
	hooks    []hooks.HookConfig
	execs    []hooks.HookExecution
	updates  []fakeUpdate
	resolveE error
}

type fakeUpdate struct {
	id      uuid.UUID
	updates map[string]any
}

func (f *fakeStore) Create(context.Context, hooks.HookConfig) (uuid.UUID, error) {
	return uuid.Nil, errors.New("not implemented")
}
func (f *fakeStore) GetByID(context.Context, uuid.UUID) (*hooks.HookConfig, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) List(context.Context, hooks.ListFilter) ([]hooks.HookConfig, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, fakeUpdate{id: id, updates: updates})
	return nil
}
func (f *fakeStore) Delete(context.Context, uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeStore) ResolveForEvent(context.Context, hooks.Event) ([]hooks.HookConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resolveE != nil {
		return nil, f.resolveE
	}
	out := make([]hooks.HookConfig, len(f.hooks))
	copy(out, f.hooks)
	return out, nil
}
func (f *fakeStore) WriteExecution(_ context.Context, e hooks.HookExecution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, e)
	return nil
}

func (f *fakeStore) snapshotExecs() []hooks.HookExecution {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]hooks.HookExecution, len(f.execs))
	copy(out, f.execs)
	return out
}

func (f *fakeStore) snapshotUpdates() []fakeUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeUpdate, len(f.updates))
	copy(out, f.updates)
	return out
}

// fakeHandler returns a scripted decision + optional sleep (for timeout tests).
type fakeHandler struct {
	decision hooks.Decision
	sleep    time.Duration
	err      error
	calls    int32
}

func (h *fakeHandler) Execute(ctx context.Context, _ hooks.HookConfig, _ hooks.Event) (hooks.Decision, error) {
	atomic.AddInt32(&h.calls, 1)
	if h.sleep > 0 {
		select {
		case <-time.After(h.sleep):
		case <-ctx.Done():
			// Respect ctx; the dispatcher maps this to DecisionTimeout.
			return hooks.DecisionTimeout, ctx.Err()
		}
	}
	return h.decision, h.err
}

func newBaseHook(ht hooks.HandlerType, ev hooks.HookEvent) hooks.HookConfig {
	return hooks.HookConfig{
		ID:          uuid.New(),
		Event:       ev,
		HandlerType: ht,
		Enabled:     true,
		Priority:    0,
		Version:     1,
		OnTimeout:   hooks.DecisionBlock,
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestDispatcher_NoHooks_ReturnsAllow(t *testing.T) {
	fs := &fakeStore{}
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store: fs,
		Audit: hooks.NewAuditWriter(fs, ""),
	})
	dec, err := d.Fire(context.Background(), hooks.Event{
		EventID:   "e1",
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow", dec)
	}
}

func TestDispatcher_SyncChain_FirstBlockWins(t *testing.T) {
	// First hook allows, second blocks, third would allow but must not run.
	allow1 := newBaseHook(hooks.HandlerHTTP, hooks.EventPreToolUse)
	block := newBaseHook(hooks.HandlerHTTP, hooks.EventPreToolUse)
	allow2 := newBaseHook(hooks.HandlerHTTP, hooks.EventPreToolUse)

	fs := &fakeStore{hooks: []hooks.HookConfig{allow1, block, allow2}}

	firstHandler := &fakeHandler{decision: hooks.DecisionAllow}
	blockHandler := &fakeHandler{decision: hooks.DecisionBlock}
	lateHandler := &fakeHandler{decision: hooks.DecisionAllow}

	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store: fs,
		Audit: hooks.NewAuditWriter(fs, ""),
		Handlers: map[hooks.HandlerType]hooks.Handler{
			hooks.HandlerHTTP: routingHandler{
				allow1.ID: firstHandler,
				block.ID:  blockHandler,
				allow2.ID: lateHandler,
			},
		},
	})
	dec, err := d.Fire(context.Background(), hooks.Event{
		EventID:   "e-block",
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block", dec)
	}
	if atomic.LoadInt32(&lateHandler.calls) != 0 {
		t.Errorf("post-block handler ran %d times, want 0", lateHandler.calls)
	}
}

// routingHandler dispatches by hook ID — lets a single test set different
// behaviors per hook while still registering as a single HandlerType.
type routingHandler map[uuid.UUID]hooks.Handler

func (r routingHandler) Execute(ctx context.Context, cfg hooks.HookConfig, ev hooks.Event) (hooks.Decision, error) {
	h, ok := r[cfg.ID]
	if !ok {
		return hooks.DecisionAllow, nil
	}
	return h.Execute(ctx, cfg, ev)
}

func TestDispatcher_PerHookTimeout_FailsClosed(t *testing.T) {
	cfg := newBaseHook(hooks.HandlerHTTP, hooks.EventPreToolUse)
	cfg.OnTimeout = hooks.DecisionBlock

	fs := &fakeStore{hooks: []hooks.HookConfig{cfg}}
	slow := &fakeHandler{decision: hooks.DecisionAllow, sleep: 200 * time.Millisecond}

	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store:          fs,
		Audit:          hooks.NewAuditWriter(fs, ""),
		Handlers:       map[hooks.HandlerType]hooks.Handler{hooks.HandlerHTTP: slow},
		PerHookTimeout: 20 * time.Millisecond,
		ChainBudget:    5 * time.Second,
	})
	dec, err := d.Fire(context.Background(), hooks.Event{
		EventID:   "e-timeout",
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (fail-closed on timeout)", dec)
	}
	// Exactly one execution row recorded with timeout decision.
	execs := fs.snapshotExecs()
	if len(execs) != 1 {
		t.Fatalf("execs=%d, want 1", len(execs))
	}
	if execs[0].Decision != hooks.DecisionTimeout {
		t.Errorf("exec decision=%q, want timeout", execs[0].Decision)
	}
}

func TestDispatcher_LoopDepth_ReturnsError(t *testing.T) {
	fs := &fakeStore{}
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store: fs,
		Audit: hooks.NewAuditWriter(fs, ""),
	})
	// Seed ctx with depth == MaxLoopDepth + 1.
	ctx := hooks.WithDepth(context.Background(), hooks.MaxLoopDepth+1)
	_, err := d.Fire(ctx, hooks.Event{EventID: "e-deep", HookEvent: hooks.EventPreToolUse})
	if !errors.Is(err, hooks.ErrLoopDepthExceeded) {
		t.Errorf("err=%v, want ErrLoopDepthExceeded", err)
	}
}

func TestDispatcher_CircuitBreaker_AutoDisable(t *testing.T) {
	cfg := newBaseHook(hooks.HandlerHTTP, hooks.EventPreToolUse)
	fs := &fakeStore{hooks: []hooks.HookConfig{cfg}}

	blocker := &fakeHandler{decision: hooks.DecisionBlock}
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store:            fs,
		Audit:            hooks.NewAuditWriter(fs, ""),
		Handlers:         map[hooks.HandlerType]hooks.Handler{hooks.HandlerHTTP: blocker},
		CircuitThreshold: 3,
		CircuitWindow:    1 * time.Minute,
	})
	// Fire threshold times — each block increments the rolling counter.
	for range 3 {
		_, _ = d.Fire(context.Background(), hooks.Event{
			EventID:   "e-cb",
			HookEvent: hooks.EventPreToolUse,
		})
	}
	// On hitting threshold the dispatcher must call store.Update to disable.
	updates := fs.snapshotUpdates()
	if len(updates) == 0 {
		t.Fatal("expected store.Update(enabled=false) after circuit breaker tripped")
	}
	last := updates[len(updates)-1]
	if last.id != cfg.ID {
		t.Errorf("update targeted %s, want %s", last.id, cfg.ID)
	}
	if v, ok := last.updates["enabled"].(bool); !ok || v {
		t.Errorf("update patch = %v, want enabled=false", last.updates)
	}
}

func TestDispatcher_NonBlockingEvent_AsyncNoDecision(t *testing.T) {
	cfg := newBaseHook(hooks.HandlerHTTP, hooks.EventPostToolUse)
	fs := &fakeStore{hooks: []hooks.HookConfig{cfg}}

	done := make(chan struct{})
	asyncHandler := &fakeHandler{decision: hooks.DecisionAllow}
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store: fs,
		Audit: hooks.NewAuditWriter(fs, ""),
		Handlers: map[hooks.HandlerType]hooks.Handler{
			hooks.HandlerHTTP: handlerFunc(func(ctx context.Context, c hooks.HookConfig, e hooks.Event) (hooks.Decision, error) {
				defer close(done)
				return asyncHandler.Execute(ctx, c, e)
			}),
		},
	})
	dec, err := d.Fire(context.Background(), hooks.Event{
		EventID:   "e-async",
		HookEvent: hooks.EventPostToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow (non-blocking path never blocks)", dec)
	}
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("async handler did not run within 500ms")
	}
}

// handlerFunc is the func-adapter — mirrors http.HandlerFunc.
type handlerFunc func(context.Context, hooks.HookConfig, hooks.Event) (hooks.Decision, error)

func (f handlerFunc) Execute(ctx context.Context, c hooks.HookConfig, e hooks.Event) (hooks.Decision, error) {
	return f(ctx, c, e)
}

func TestDispatcher_AllAllow_ReturnsAllow(t *testing.T) {
	a := newBaseHook(hooks.HandlerHTTP, hooks.EventPreToolUse)
	b := newBaseHook(hooks.HandlerHTTP, hooks.EventPreToolUse)
	fs := &fakeStore{hooks: []hooks.HookConfig{a, b}}
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store:    fs,
		Audit:    hooks.NewAuditWriter(fs, ""),
		Handlers: map[hooks.HandlerType]hooks.Handler{hooks.HandlerHTTP: &fakeHandler{decision: hooks.DecisionAllow}},
	})
	dec, err := d.Fire(context.Background(), hooks.Event{
		EventID:   "e-all-allow",
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow", dec)
	}
	if got := len(fs.snapshotExecs()); got != 2 {
		t.Errorf("exec rows=%d, want 2 (one per hook)", got)
	}
}

func TestDispatcher_ResolveError_FailsClosed(t *testing.T) {
	fs := &fakeStore{resolveE: errors.New("db down")}
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store: fs,
		Audit: hooks.NewAuditWriter(fs, ""),
	})
	dec, err := d.Fire(context.Background(), hooks.Event{
		EventID:   "e-resolve-err",
		HookEvent: hooks.EventPreToolUse,
	})
	if err == nil {
		t.Fatal("Fire err=nil, want non-nil on resolve failure")
	}
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (fail-closed)", dec)
	}
}

func TestDispatcher_MissingHandler_BlocksBlockingEvent(t *testing.T) {
	// Hook requests `command` handler but dispatcher has none registered.
	// Blocking events must fail-closed.
	cfg := newBaseHook(hooks.HandlerCommand, hooks.EventPreToolUse)
	fs := &fakeStore{hooks: []hooks.HookConfig{cfg}}
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store: fs,
		Audit: hooks.NewAuditWriter(fs, ""),
	})
	dec, _ := d.Fire(context.Background(), hooks.Event{
		EventID:   "e-no-handler",
		HookEvent: hooks.EventPreToolUse,
	})
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (missing handler → fail-closed)", dec)
	}
}

func TestDispatcher_DedupKey_IncludesEventID(t *testing.T) {
	cfg := newBaseHook(hooks.HandlerHTTP, hooks.EventPreToolUse)
	fs := &fakeStore{hooks: []hooks.HookConfig{cfg}}
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store:    fs,
		Audit:    hooks.NewAuditWriter(fs, ""),
		Handlers: map[hooks.HandlerType]hooks.Handler{hooks.HandlerHTTP: &fakeHandler{decision: hooks.DecisionAllow}},
	})
	_, _ = d.Fire(context.Background(), hooks.Event{
		EventID:   "evt-xyz",
		HookEvent: hooks.EventPreToolUse,
	})
	execs := fs.snapshotExecs()
	if len(execs) != 1 {
		t.Fatalf("execs=%d, want 1", len(execs))
	}
	if execs[0].DedupKey == "" {
		t.Error("dedup_key empty, want populated")
	}
}

func TestNoopDispatcher_AlwaysAllows(t *testing.T) {
	d := hooks.NewNoopDispatcher()
	dec, err := d.Fire(context.Background(), hooks.Event{
		EventID:   "e",
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow", dec)
	}
}
