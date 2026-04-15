package hooks

import (
	"context"

	"github.com/google/uuid"
)

// ListFilter narrows results from HookStore.List.
type ListFilter struct {
	TenantID *uuid.UUID
	AgentID  *uuid.UUID
	Event    *HookEvent
	Scope    *Scope
	Enabled  *bool
}

// HookStore is the data-access contract for agent hooks.
// PG impl lives in internal/store/pg/hooks.go;
// SQLite impl lives in internal/store/sqlitestore/hooks.go.
//
// All write operations must enforce tenant isolation: reads outside master scope
// must include WHERE tenant_id = $N. See store.IsMasterScope(ctx).
type HookStore interface {
	// Create inserts a new hook config and returns the generated UUID.
	Create(ctx context.Context, cfg HookConfig) (uuid.UUID, error)

	// GetByID returns a single hook config by primary key.
	// Returns nil, nil when not found (no sentinel error).
	GetByID(ctx context.Context, id uuid.UUID) (*HookConfig, error)

	// List returns hooks matching the filter. Caller applies ordering/limit.
	List(ctx context.Context, filter ListFilter) ([]HookConfig, error)

	// Update applies the map of column→value patches and bumps the version field.
	// Callers must not include "version" in updates — the store increments it
	// atomically to bust the TTL cache entry (H1 mitigation).
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error

	// Delete removes a hook config. hook_executions rows are preserved via
	// ON DELETE SET NULL on the hook_id FK column.
	Delete(ctx context.Context, id uuid.UUID) error

	// ResolveForEvent returns the ordered list of enabled hooks that match
	// (tenant_id, agent_id, event). Implementations should cache this result
	// with a short TTL (5s) keyed by (tenant_id, agent_id, event, maxVersion)
	// and short-circuit via COUNT(*) = 0 cache for the zero-hooks hot path.
	ResolveForEvent(ctx context.Context, event Event) ([]HookConfig, error)

	// WriteExecution appends an immutable execution audit row.
	// Caller must pre-truncate Error to 256 chars and encrypt ErrorDetail.
	WriteExecution(ctx context.Context, exec HookExecution) error
}
