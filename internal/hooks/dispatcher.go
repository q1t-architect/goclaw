package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ── Public surface ───────────────────────────────────────────────────────────

// Handler executes a single hook config against an event. Returned Decision
// drives the blocking-path outcome; the audit writer records the row.
type Handler interface {
	Execute(ctx context.Context, cfg HookConfig, ev Event) (Decision, error)
}

// Dispatcher is the pipeline integration surface. Stages call Fire and act on
// the Decision (allow → continue; block → abort with i18n error).
type Dispatcher interface {
	Fire(ctx context.Context, ev Event) (Decision, error)
}

// MaxLoopDepth caps nested hook invocation (M5). Depth increments when a hook
// triggers a sub-agent whose own events feed back into the dispatcher.
const MaxLoopDepth = 3

// ErrLoopDepthExceeded signals the M5 circuit: refuse to process further
// events once the chain exceeds MaxLoopDepth.
var ErrLoopDepthExceeded = errors.New("hooks: loop depth exceeded")

// Defaults for the dispatcher knobs. Set via StdDispatcherOpts when a test or
// deployment needs tighter timings (unit tests use sub-second values).
const (
	defaultPerHookTimeout   = 5 * time.Second
	defaultChainBudget      = 10 * time.Second
	defaultCircuitThreshold = 5
	defaultCircuitWindow    = 1 * time.Minute
)

// ctxDepthKey is the context-value key for the nested-hook depth counter.
// Private type prevents collisions with other packages' context keys.
type ctxDepthKey struct{}

// WithDepth stores d in ctx; Fire reads it to enforce MaxLoopDepth (M5).
// Callers that dispatch sub-agent events must increment before re-entering.
func WithDepth(ctx context.Context, d int) context.Context {
	return context.WithValue(ctx, ctxDepthKey{}, d)
}

func depthFromCtx(ctx context.Context) int {
	if v, ok := ctx.Value(ctxDepthKey{}).(int); ok {
		return v
	}
	return 0
}

// DepthFrom returns the current hook loop depth from ctx (0 if unset).
func DepthFrom(ctx context.Context) int { return depthFromCtx(ctx) }

// IncDepth returns a new ctx with depth incremented by 1.
// Callers wrapping sub-agent invocations must call this before re-entering.
func IncDepth(ctx context.Context) context.Context {
	return WithDepth(ctx, depthFromCtx(ctx)+1)
}

// StdDispatcherOpts configures the production dispatcher. Unset fields fall
// back to the default* constants above.
type StdDispatcherOpts struct {
	Store    HookStore
	Audit    *AuditWriter
	Handlers map[HandlerType]Handler

	PerHookTimeout time.Duration
	ChainBudget    time.Duration

	CircuitThreshold int
	CircuitWindow    time.Duration

	// Now is injectable for tests that need deterministic circuit-breaker timing.
	Now func() time.Time
}

// NewStdDispatcher returns the production Dispatcher with circuit-breaker,
// per-hook timeouts, and audit writing wired up.
func NewStdDispatcher(opts StdDispatcherOpts) Dispatcher {
	if opts.PerHookTimeout <= 0 {
		opts.PerHookTimeout = defaultPerHookTimeout
	}
	if opts.ChainBudget <= 0 {
		opts.ChainBudget = defaultChainBudget
	}
	if opts.CircuitThreshold <= 0 {
		opts.CircuitThreshold = defaultCircuitThreshold
	}
	if opts.CircuitWindow <= 0 {
		opts.CircuitWindow = defaultCircuitWindow
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Handlers == nil {
		opts.Handlers = map[HandlerType]Handler{}
	}
	return &stdDispatcher{
		store:       opts.Store,
		audit:       opts.Audit,
		handlers:    opts.Handlers,
		perTimeout:  opts.PerHookTimeout,
		chainBudget: opts.ChainBudget,
		now:         opts.Now,
		cb: &circuitBreaker{
			threshold: opts.CircuitThreshold,
			window:    opts.CircuitWindow,
			hits:      map[uuid.UUID][]time.Time{},
			tripped:   map[uuid.UUID]bool{},
		},
	}
}

// NewNoopDispatcher returns a dispatcher that always allows. Used when the
// hook system is disabled (e.g. CI without DB) or before init completes.
func NewNoopDispatcher() Dispatcher {
	return noopDispatcher{}
}

type noopDispatcher struct{}

func (noopDispatcher) Fire(context.Context, Event) (Decision, error) {
	return DecisionAllow, nil
}

// ── stdDispatcher ────────────────────────────────────────────────────────────

type stdDispatcher struct {
	store       HookStore
	audit       *AuditWriter
	handlers    map[HandlerType]Handler
	perTimeout  time.Duration
	chainBudget time.Duration
	now         func() time.Time
	cb          *circuitBreaker
}

func (d *stdDispatcher) Fire(ctx context.Context, ev Event) (Decision, error) {
	if depthFromCtx(ctx) > MaxLoopDepth {
		slog.Warn("security.hook.loop_depth_exceeded",
			"event_id", ev.EventID,
			"hook_event", ev.HookEvent,
		)
		return DecisionError, ErrLoopDepthExceeded
	}

	hooks, err := d.store.ResolveForEvent(ctx, ev)
	if err != nil {
		// Fail-closed: a DB blip must not let a pre-tool gate open silently.
		slog.Warn("security.hook.resolve_error", "err", err, "event_id", ev.EventID)
		if ev.HookEvent.IsBlocking() {
			return DecisionBlock, err
		}
		return DecisionAllow, err
	}
	if len(hooks) == 0 {
		return DecisionAllow, nil
	}

	if ev.HookEvent.IsBlocking() {
		return d.runSync(ctx, ev, hooks)
	}
	d.runAsync(ctx, ev, hooks)
	return DecisionAllow, nil
}

// runSync executes the blocking chain with a wall-time budget and per-hook
// timeouts. First block wins; any fail-closed condition aborts to Block.
func (d *stdDispatcher) runSync(ctx context.Context, ev Event, chain []HookConfig) (Decision, error) {
	chainCtx, cancel := context.WithTimeout(ctx, d.chainBudget)
	defer cancel()

	for _, cfg := range chain {
		if !cfg.Enabled {
			continue
		}
		if d.cb.isTripped(cfg.ID, d.now()) {
			d.writeExec(ctx, cfg, ev, DecisionBlock, 0, "circuit breaker open")
			return DecisionBlock, nil
		}
		if !d.prefilter(cfg, ev) {
			continue
		}
		dec, execErr, duration := d.runOne(chainCtx, cfg, ev)
		errMsg := ""
		if execErr != nil {
			errMsg = execErr.Error()
		}
		d.writeExec(ctx, cfg, ev, dec, duration, errMsg)

		switch dec {
		case DecisionBlock:
			d.cb.record(cfg.ID, d.now(), d.store)
			return DecisionBlock, nil
		case DecisionTimeout:
			d.cb.record(cfg.ID, d.now(), d.store)
			if cfg.OnTimeout == DecisionBlock {
				return DecisionBlock, nil
			}
			// OnTimeout=allow: degrade gracefully but keep scanning.
		case DecisionError:
			// Unexpected error in a blocking chain → fail-closed.
			return DecisionBlock, nil
		}

		if chainCtx.Err() != nil {
			// Chain wall-time budget exhausted (H3): fail-closed.
			return DecisionBlock, nil
		}
	}
	return DecisionAllow, nil
}

// runAsync fires non-blocking hooks concurrently. Phase 1 uses a simple
// goroutine-per-hook; Phase 2 will route through the eventbus worker pool.
func (d *stdDispatcher) runAsync(ctx context.Context, ev Event, chain []HookConfig) {
	for _, cfg := range chain {
		if !cfg.Enabled || !d.prefilter(cfg, ev) {
			continue
		}
		go func(c HookConfig) {
			dec, execErr, duration := d.runOne(ctx, c, ev)
			errMsg := ""
			if execErr != nil {
				errMsg = execErr.Error()
			}
			d.writeExec(context.Background(), c, ev, dec, duration, errMsg)
		}(cfg)
	}
}

// runOne executes a single hook with its per-hook timeout. Returns the
// decision, any error from the handler, and the elapsed duration.
func (d *stdDispatcher) runOne(ctx context.Context, cfg HookConfig, ev Event) (Decision, error, time.Duration) {
	handler, ok := d.handlers[cfg.HandlerType]
	if !ok {
		return DecisionError, fmt.Errorf("hook: no handler registered for %q", cfg.HandlerType), 0
	}

	timeout := d.perTimeout
	if cfg.TimeoutMS > 0 {
		timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
	hctx, hcancel := context.WithTimeout(ctx, timeout)
	defer hcancel()

	start := d.now()
	dec, err := handler.Execute(hctx, cfg, ev)
	duration := d.now().Sub(start)

	if errors.Is(hctx.Err(), context.DeadlineExceeded) {
		return DecisionTimeout, hctx.Err(), duration
	}
	return dec, err, duration
}

// prefilter applies the matcher + CEL gate; Phase 1 keeps it lightweight and
// pushes the actual compile through matcher.go's cached helpers.
func (d *stdDispatcher) prefilter(cfg HookConfig, ev Event) bool {
	if cfg.Matcher != "" {
		re, err := CompileMatcher(cfg.Matcher)
		if err != nil || !MatchToolName(re, ev.ToolName) {
			return false
		}
	}
	if cfg.IfExpr != "" {
		prg, err := CompileCELExpr(cfg.IfExpr)
		if err != nil {
			return false
		}
		ok, err := EvalCEL(prg, map[string]any{
			"tool_name":  ev.ToolName,
			"tool_input": ev.ToolInput,
			"depth":      ev.Depth,
		})
		if err != nil || !ok {
			return false
		}
	}
	return true
}

// writeExec assembles the audit row and routes it through AuditWriter which
// handles truncation, redaction, and encryption. Failures are logged but do
// not propagate — audit is observability, not policy.
func (d *stdDispatcher) writeExec(ctx context.Context, cfg HookConfig, ev Event, dec Decision, duration time.Duration, errMsg string) {
	if d.audit == nil {
		return
	}
	inputHash, _ := CanonicalInputHash(ev.ToolName, ev.ToolInput)
	hookID := cfg.ID
	exec := HookExecution{
		ID:         uuid.New(),
		HookID:     &hookID,
		SessionID:  ev.SessionID,
		Event:      ev.HookEvent,
		InputHash:  inputHash,
		Decision:   dec,
		DurationMS: int(duration / time.Millisecond),
		DedupKey:   cfg.ID.String() + ":" + ev.EventID,
		Error:      errMsg,
		Metadata:   map[string]any{},
		CreatedAt:  d.now(),
	}
	if err := d.audit.Log(ctx, exec); err != nil {
		slog.Warn("security.hook.audit_write_failed", "err", err, "hook_id", cfg.ID)
	}
}

// ── circuitBreaker ───────────────────────────────────────────────────────────

// circuitBreaker tracks recent block/timeout timestamps per hook; once the
// count inside the rolling window hits the threshold it persists the hook as
// disabled and short-circuits subsequent Fires (C4 mitigation).
type circuitBreaker struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	hits      map[uuid.UUID][]time.Time
	tripped   map[uuid.UUID]bool
}

func (cb *circuitBreaker) isTripped(id uuid.UUID, _ time.Time) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.tripped[id]
}

// record appends a block/timeout event and, if the window count hits the
// threshold, trips the breaker and asks the store to persist enabled=false.
// Persistence failure is logged only — the in-memory trip still protects the
// current process.
func (cb *circuitBreaker) record(id uuid.UUID, now time.Time, store HookStore) {
	cb.mu.Lock()
	cutoff := now.Add(-cb.window)
	kept := cb.hits[id][:0]
	for _, t := range cb.hits[id] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	cb.hits[id] = kept
	justTripped := !cb.tripped[id] && len(kept) >= cb.threshold
	if justTripped {
		cb.tripped[id] = true
	}
	cb.mu.Unlock()

	if !justTripped {
		return
	}
	slog.Warn("security.hook.circuit_breaker",
		"hook_id", id,
		"window", cb.window.String(),
		"threshold", cb.threshold,
	)
	if store == nil {
		return
	}
	if err := store.Update(context.Background(), id, map[string]any{"enabled": false}); err != nil {
		slog.Warn("security.hook.circuit_breaker_persist_failed", "hook_id", id, "err", err)
	}
}
