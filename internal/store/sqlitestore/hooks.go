//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SqliteHookStore implements hooks.HookStore backed by SQLite.
type SqliteHookStore struct {
	db *sql.DB

	cacheMu sync.Mutex
	cache   map[string]sqliteHookCacheEntry
}

type sqliteHookCacheEntry struct {
	result     []hooks.HookConfig
	maxVersion int
	expiresAt  time.Time
}

const sqliteHookCacheTTL = 5 * time.Second

// NewSQLiteHookStore returns a SqliteHookStore backed by the given *sql.DB.
func NewSQLiteHookStore(db *sql.DB) *SqliteHookStore {
	return &SqliteHookStore{
		db:    db,
		cache: make(map[string]sqliteHookCacheEntry),
	}
}

// ─── Create ─────────────────────────────────────────────────────────────────

func (s *SqliteHookStore) Create(ctx context.Context, cfg hooks.HookConfig) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC().Format(time.RFC3339Nano)

	cfgJSON, err := json.Marshal(cfg.Config)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal config: %w", err)
	}
	metaJSON, err := json.Marshal(cfg.Metadata)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal metadata: %w", err)
	}

	tid := cfg.TenantID
	if tid == uuid.Nil {
		tid = tenantIDForInsert(ctx)
	}

	var agentID, createdBy, matcher, ifExpr *string
	if cfg.AgentID != nil {
		s := cfg.AgentID.String()
		agentID = &s
	}
	if cfg.CreatedBy != nil {
		s := cfg.CreatedBy.String()
		createdBy = &s
	}
	if cfg.Matcher != "" {
		matcher = &cfg.Matcher
	}
	if cfg.IfExpr != "" {
		ifExpr = &cfg.IfExpr
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agent_hooks
		  (id, tenant_id, agent_id, scope, event, handler_type,
		   config, matcher, if_expr, timeout_ms, on_timeout,
		   priority, enabled, version, source, metadata, created_by,
		   created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?,?,?)`,
		id.String(), tid.String(), agentID,
		string(cfg.Scope), string(cfg.Event), string(cfg.HandlerType),
		string(cfgJSON), matcher, ifExpr,
		cfg.TimeoutMS, string(cfg.OnTimeout),
		cfg.Priority, cfg.Enabled,
		string(cfg.Source), string(metaJSON), createdBy,
		now, now,
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert hook: %w", err)
	}
	s.invalidateCache()
	return id, nil
}

// ─── GetByID ─────────────────────────────────────────────────────────────────

func (s *SqliteHookStore) GetByID(ctx context.Context, id uuid.UUID) (*hooks.HookConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, scope, event, handler_type,
		       config, matcher, if_expr, timeout_ms, on_timeout,
		       priority, enabled, version, source, metadata, created_by,
		       created_at, updated_at
		FROM agent_hooks WHERE id = ?`, id.String())
	cfg, err := scanHookSQLiteRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get hook by id: %w", err)
	}
	return cfg, nil
}

// ─── List ────────────────────────────────────────────────────────────────────

func (s *SqliteHookStore) List(ctx context.Context, filter hooks.ListFilter) ([]hooks.HookConfig, error) {
	q := `SELECT id, tenant_id, agent_id, scope, event, handler_type,
		       config, matcher, if_expr, timeout_ms, on_timeout,
		       priority, enabled, version, source, metadata, created_by,
		       created_at, updated_at FROM agent_hooks WHERE 1=1`
	var args []any

	if !store.IsMasterScope(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required for non-master scope")
		}
		q += " AND (tenant_id = ? OR tenant_id = ?)"
		args = append(args, tid.String(), store.MasterTenantID.String())
	} else if filter.TenantID != nil {
		q += " AND tenant_id = ?"
		args = append(args, filter.TenantID.String())
	}

	if filter.AgentID != nil {
		q += " AND agent_id = ?"
		args = append(args, filter.AgentID.String())
	}
	if filter.Event != nil {
		q += " AND event = ?"
		args = append(args, string(*filter.Event))
	}
	if filter.Scope != nil {
		q += " AND scope = ?"
		args = append(args, string(*filter.Scope))
	}
	if filter.Enabled != nil {
		val := 0
		if *filter.Enabled {
			val = 1
		}
		q += " AND enabled = ?"
		args = append(args, val)
	}
	q += " ORDER BY priority DESC, created_at ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list hooks: %w", err)
	}
	defer rows.Close()

	var result []hooks.HookConfig
	for rows.Next() {
		cfg, err := scanHookSQLiteRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *cfg)
	}
	return result, rows.Err()
}

// ─── Update ──────────────────────────────────────────────────────────────────

func (s *SqliteHookStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if _, ok := updates["version"]; ok {
		return fmt.Errorf("callers must not include 'version' in update map")
	}

	// Marshal map/slice values to JSON strings for SQLite TEXT columns.
	for k, v := range updates {
		switch k {
		case "config", "metadata":
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("marshal %s: %w", k, err)
			}
			updates[k] = string(b)
		}
	}

	// Build SET clause with version bump.
	var setClauses []string
	var args []any
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, col+" = ?")
		args = append(args, val)
	}
	// Always bump version and updated_at atomically.
	setClauses = append(setClauses, "version = version + 1, updated_at = ?")
	args = append(args, time.Now().UTC().Format(time.RFC3339Nano))

	args = append(args, id.String())
	q := fmt.Sprintf("UPDATE agent_hooks SET %s WHERE id = ?",
		strings.Join(setClauses, ", "))

	if !store.IsMasterScope(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required for non-master scope")
		}
		q += " AND tenant_id = ?"
		args = append(args, tid.String())
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("hook not found: %s", id)
	}
	s.invalidateCache()
	return nil
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func (s *SqliteHookStore) Delete(ctx context.Context, id uuid.UUID) error {
	q := "DELETE FROM agent_hooks WHERE id = ?"
	args := []any{id.String()}

	if !store.IsMasterScope(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required for non-master scope")
		}
		q += " AND tenant_id = ?"
		args = append(args, tid.String())
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("delete hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("hook not found: %s", id)
	}
	s.invalidateCache()
	return nil
}

// ─── ResolveForEvent ─────────────────────────────────────────────────────────

func (s *SqliteHookStore) ResolveForEvent(ctx context.Context, event hooks.Event) ([]hooks.HookConfig, error) {
	tenantID := event.TenantID
	agentID := event.AgentID
	hookEvent := event.HookEvent

	// Check max version to validate cache freshness.
	var maxVersion int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0) FROM agent_hooks
		 WHERE enabled = 1 AND event = ?
		   AND (tenant_id = ? OR tenant_id = ?)
		   AND (agent_id = ? OR agent_id IS NULL)`,
		string(hookEvent), tenantID.String(), store.MasterTenantID.String(), agentID.String(),
	).Scan(&maxVersion)
	if err != nil {
		return nil, fmt.Errorf("resolve version check: %w", err)
	}

	key := sqliteHookResolveKey(tenantID, agentID, hookEvent)
	s.cacheMu.Lock()
	entry, ok := s.cache[key]
	s.cacheMu.Unlock()

	if ok && time.Now().Before(entry.expiresAt) && entry.maxVersion == maxVersion {
		return entry.result, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agent_id, scope, event, handler_type,
		       config, matcher, if_expr, timeout_ms, on_timeout,
		       priority, enabled, version, source, metadata, created_by,
		       created_at, updated_at
		FROM agent_hooks
		WHERE enabled = 1 AND event = ?
		  AND (tenant_id = ? OR tenant_id = ?)
		  AND (agent_id = ? OR agent_id IS NULL)
		ORDER BY priority DESC, created_at ASC`,
		string(hookEvent), tenantID.String(), store.MasterTenantID.String(), agentID.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("resolve hooks: %w", err)
	}
	defer rows.Close()

	var result []hooks.HookConfig
	for rows.Next() {
		cfg, err := scanHookSQLiteRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.cacheMu.Lock()
	s.cache[key] = sqliteHookCacheEntry{
		result:     result,
		maxVersion: maxVersion,
		expiresAt:  time.Now().Add(sqliteHookCacheTTL),
	}
	s.cacheMu.Unlock()

	return result, nil
}

// ─── WriteExecution ──────────────────────────────────────────────────────────

func (s *SqliteHookStore) WriteExecution(ctx context.Context, exec hooks.HookExecution) error {
	metaJSON, err := json.Marshal(exec.Metadata)
	if err != nil {
		return fmt.Errorf("marshal exec metadata: %w", err)
	}

	now := exec.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var hookID, dedupKey, sessionID, inputHash, errStr *string
	if exec.HookID != nil {
		s := exec.HookID.String()
		hookID = &s
	}
	if exec.DedupKey != "" {
		dedupKey = &exec.DedupKey
	}
	if exec.SessionID != "" {
		sessionID = &exec.SessionID
	}
	if exec.InputHash != "" {
		inputHash = &exec.InputHash
	}
	if exec.Error != "" {
		errStr = &exec.Error
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO hook_executions
		  (id, hook_id, session_id, event, input_hash, decision,
		   duration_ms, retry, dedup_key, error, error_detail, metadata, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		exec.ID.String(), hookID, sessionID, string(exec.Event),
		inputHash, string(exec.Decision),
		exec.DurationMS, exec.Retry, dedupKey,
		errStr, exec.ErrorDetail, string(metaJSON),
		now.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("write execution: %w", err)
	}
	return nil
}

// ─── cache helpers ───────────────────────────────────────────────────────────

func (s *SqliteHookStore) invalidateCache() {
	s.cacheMu.Lock()
	s.cache = make(map[string]sqliteHookCacheEntry)
	s.cacheMu.Unlock()
}

func sqliteHookResolveKey(tenantID, agentID uuid.UUID, event hooks.HookEvent) string {
	return tenantID.String() + "|" + agentID.String() + "|" + string(event)
}

// ─── scan helper ─────────────────────────────────────────────────────────────

type sqliteRowScanner interface {
	Scan(dest ...any) error
}

func scanHookSQLiteRow(row sqliteRowScanner) (*hooks.HookConfig, error) {
	var (
		cfg                    hooks.HookConfig
		idStr, tenantStr       string
		agentIDStr             sql.NullString
		createdByStr           sql.NullString
		scope, event           string
		handlerType, onTimeout string
		source                 string
		matcher, ifExpr        sql.NullString
		cfgStr, metaStr        string
		enabledInt             int
		createdAt, updatedAt   sqliteTime
	)
	err := row.Scan(
		&idStr, &tenantStr, &agentIDStr,
		&scope, &event, &handlerType,
		&cfgStr, &matcher, &ifExpr,
		&cfg.TimeoutMS, &onTimeout,
		&cfg.Priority, &enabledInt, &cfg.Version,
		&source, &metaStr, &createdByStr,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	if parsed, err := uuid.Parse(idStr); err == nil {
		cfg.ID = parsed
	}
	if parsed, err := uuid.Parse(tenantStr); err == nil {
		cfg.TenantID = parsed
	}
	if agentIDStr.Valid {
		if parsed, err := uuid.Parse(agentIDStr.String); err == nil {
			cfg.AgentID = &parsed
		}
	}
	if createdByStr.Valid {
		if parsed, err := uuid.Parse(createdByStr.String); err == nil {
			cfg.CreatedBy = &parsed
		}
	}

	cfg.Scope = hooks.Scope(scope)
	cfg.Event = hooks.HookEvent(event)
	cfg.HandlerType = hooks.HandlerType(handlerType)
	cfg.OnTimeout = hooks.Decision(onTimeout)
	cfg.Source = source
	cfg.Enabled = enabledInt != 0
	cfg.CreatedAt = createdAt.Time
	cfg.UpdatedAt = updatedAt.Time

	if matcher.Valid {
		cfg.Matcher = matcher.String
	}
	if ifExpr.Valid {
		cfg.IfExpr = ifExpr.String
	}

	if cfgStr != "" {
		_ = json.Unmarshal([]byte(cfgStr), &cfg.Config)
	}
	if cfg.Config == nil {
		cfg.Config = map[string]any{}
	}
	if metaStr != "" {
		_ = json.Unmarshal([]byte(metaStr), &cfg.Metadata)
	}
	if cfg.Metadata == nil {
		cfg.Metadata = map[string]any{}
	}

	return &cfg, nil
}
