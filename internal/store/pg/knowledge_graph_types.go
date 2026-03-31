package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ErrEntityTypeInUse is returned when trying to delete an entity type that has entities.
var ErrEntityTypeInUse = fmt.Errorf("entity type is in use")

// ErrRelationTypeInUse is returned when trying to delete a relation type that has relations.
var ErrRelationTypeInUse = fmt.Errorf("relation type is in use")

// --- Entity Types ---

func (s *PGKnowledgeGraphStore) GetEntityTypes(ctx context.Context, agentID string) ([]store.EntityType, error) {
	aid := mustParseUUID(agentID)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, name, display_name, color, icon, description,
		       properties_schema, is_system, sort_order, created_at, updated_at
		FROM kg_entity_types
		WHERE agent_id = $1
		ORDER BY sort_order, name`, aid)
	if err != nil {
		return nil, fmt.Errorf("query entity types: %w", err)
	}
	defer rows.Close()

	var types []store.EntityType
	for rows.Next() {
		var et store.EntityType
		var schemaBytes []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&et.ID, &et.AgentID, &et.Name, &et.DisplayName, &et.Color, &et.Icon,
			&et.Description, &schemaBytes, &et.IsSystem, &et.SortOrder,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan entity type: %w", err)
		}
		et.CreatedAt = createdAt.UnixMilli()
		et.UpdatedAt = updatedAt.UnixMilli()
		if len(schemaBytes) > 0 && string(schemaBytes) != "[]" {
			_ = json.Unmarshal(schemaBytes, &et.PropertiesSchema)
		}
		types = append(types, et)
	}
	return types, rows.Err()
}

// CountEntitiesByType returns the number of entities using a given entity type.
func (s *PGKnowledgeGraphStore) CountEntitiesByType(ctx context.Context, agentID, typeID string) (int64, error) {
	aid := mustParseUUID(agentID)
	tid := mustParseUUID(typeID)
	var count int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_entities WHERE agent_id = $1 AND entity_type = (SELECT name FROM kg_entity_types WHERE id = $2)`,
		aid, tid,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count entities by type: %w", err)
	}
	return count, nil
}

func (s *PGKnowledgeGraphStore) UpsertEntityType(ctx context.Context, et *store.EntityType) error {
	aid := mustParseUUID(et.AgentID)
	schema, _ := json.Marshal(et.PropertiesSchema)
	if schema == nil {
		schema = []byte("[]")
	}
	now := time.Now()
	id := uuid.Must(uuid.NewV7())

	var actualID uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO kg_entity_types
			(id, agent_id, name, display_name, color, icon, description,
			 properties_schema, is_system, sort_order, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
		ON CONFLICT (agent_id, name) DO UPDATE SET
			display_name      = EXCLUDED.display_name,
			color             = EXCLUDED.color,
			icon              = EXCLUDED.icon,
			description       = EXCLUDED.description,
			properties_schema = EXCLUDED.properties_schema,
			sort_order        = EXCLUDED.sort_order,
			updated_at        = EXCLUDED.updated_at
		RETURNING id`,
		id, aid, et.Name, et.DisplayName, et.Color, et.Icon, et.Description,
		schema, et.IsSystem, et.SortOrder, now,
	).Scan(&actualID); err != nil {
		return fmt.Errorf("upsert entity type: %w", err)
	}

	et.ID = actualID.String()
	et.CreatedAt = now.UnixMilli()
	et.UpdatedAt = now.UnixMilli()
	return nil
}

func (s *PGKnowledgeGraphStore) DeleteEntityType(ctx context.Context, agentID, typeID string) error {
	aid := mustParseUUID(agentID)
	tid := mustParseUUID(typeID)

	// Prevent deletion of system types
	var isSystem bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT is_system FROM kg_entity_types WHERE id = $1 AND agent_id = $2`,
		tid, aid,
	).Scan(&isSystem); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("entity type not found")
		}
		return fmt.Errorf("check entity type: %w", err)
	}
	if isSystem {
		return fmt.Errorf("cannot delete system entity type")
	}

	// Check if any entities are using this type
	count, err := s.CountEntitiesByType(ctx, agentID, typeID)
	if err != nil {
		return fmt.Errorf("check entity type usage: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("%w: %d entities are using it", ErrEntityTypeInUse, count)
	}

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM kg_entity_types WHERE id = $1 AND agent_id = $2`,
		tid, aid)
	if err != nil {
		return fmt.Errorf("delete entity type: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entity type not found")
	}
	return nil
}

// --- Relation Types ---

func (s *PGKnowledgeGraphStore) GetRelationTypes(ctx context.Context, agentID string) ([]store.RelationType, error) {
	aid := mustParseUUID(agentID)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, name, display_name, color, directed, description,
		       properties_schema, is_system, sort_order, created_at, updated_at
		FROM kg_relation_types
		WHERE agent_id = $1
		ORDER BY sort_order, name`, aid)
	if err != nil {
		return nil, fmt.Errorf("query relation types: %w", err)
	}
	defer rows.Close()

	var types []store.RelationType
	for rows.Next() {
		var rt store.RelationType
		var schemaBytes []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&rt.ID, &rt.AgentID, &rt.Name, &rt.DisplayName, &rt.Color, &rt.Directed,
			&rt.Description, &schemaBytes, &rt.IsSystem, &rt.SortOrder,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan relation type: %w", err)
		}
		rt.CreatedAt = createdAt.UnixMilli()
		rt.UpdatedAt = updatedAt.UnixMilli()
		if len(schemaBytes) > 0 && string(schemaBytes) != "[]" {
			_ = json.Unmarshal(schemaBytes, &rt.PropertiesSchema)
		}
		types = append(types, rt)
	}
	return types, rows.Err()
}

// CountRelationsByType returns the number of relations using a given relation type.
func (s *PGKnowledgeGraphStore) CountRelationsByType(ctx context.Context, agentID, typeID string) (int64, error) {
	aid := mustParseUUID(agentID)
	tid := mustParseUUID(typeID)
	var count int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_relations WHERE agent_id = $1 AND relation_type = (SELECT name FROM kg_relation_types WHERE id = $2)`,
		aid, tid,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count relations by type: %w", err)
	}
	return count, nil
}

func (s *PGKnowledgeGraphStore) UpsertRelationType(ctx context.Context, rt *store.RelationType) error {
	aid := mustParseUUID(rt.AgentID)
	schema, _ := json.Marshal(rt.PropertiesSchema)
	if schema == nil {
		schema = []byte("[]")
	}
	now := time.Now()
	id := uuid.Must(uuid.NewV7())

	var actualID uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO kg_relation_types
			(id, agent_id, name, display_name, color, directed, description,
			 properties_schema, is_system, sort_order, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
		ON CONFLICT (agent_id, name) DO UPDATE SET
			display_name      = EXCLUDED.display_name,
			color             = EXCLUDED.color,
			directed          = EXCLUDED.directed,
			description       = EXCLUDED.description,
			properties_schema = EXCLUDED.properties_schema,
			sort_order        = EXCLUDED.sort_order,
			updated_at        = EXCLUDED.updated_at
		RETURNING id`,
		id, aid, rt.Name, rt.DisplayName, rt.Color, rt.Directed, rt.Description,
		schema, rt.IsSystem, rt.SortOrder, now,
	).Scan(&actualID); err != nil {
		return fmt.Errorf("upsert relation type: %w", err)
	}

	rt.ID = actualID.String()
	rt.CreatedAt = now.UnixMilli()
	rt.UpdatedAt = now.UnixMilli()
	return nil
}

func (s *PGKnowledgeGraphStore) DeleteRelationType(ctx context.Context, agentID, typeID string) error {
	aid := mustParseUUID(agentID)
	tid := mustParseUUID(typeID)

	// Prevent deletion of system types
	var isSystem bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT is_system FROM kg_relation_types WHERE id = $1 AND agent_id = $2`,
		tid, aid,
	).Scan(&isSystem); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("relation type not found")
		}
		return fmt.Errorf("check relation type: %w", err)
	}
	if isSystem {
		return fmt.Errorf("cannot delete system relation type")
	}

	// Check if any relations are using this type
	count, err := s.CountRelationsByType(ctx, agentID, typeID)
	if err != nil {
		return fmt.Errorf("check relation type usage: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("%w: %d relations are using it", ErrRelationTypeInUse, count)
	}

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM kg_relation_types WHERE id = $1 AND agent_id = $2`,
		tid, aid)
	if err != nil {
		return fmt.Errorf("delete relation type: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("relation type not found")
	}
	return nil
}

// --- Seed ---

func (s *PGKnowledgeGraphStore) SeedKGTypes(ctx context.Context, agentID string, preset string) error {
	aid := mustParseUUID(agentID)
	switch preset {
	case "general":
		_, err := s.db.ExecContext(ctx, `SELECT seed_kg_default_types($1)`, aid)
		return err
	case "legal":
		return s.seedLegalTypes(ctx, aid)
	case "development":
		return s.seedDevTypes(ctx, aid)
	case "empty":
		return nil
	default:
		_, err := s.db.ExecContext(ctx, `SELECT seed_kg_default_types($1)`, aid)
		return err
	}
}

func (s *PGKnowledgeGraphStore) seedLegalTypes(ctx context.Context, aid uuid.UUID) error {
	// Entity types
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kg_entity_types (agent_id, name, display_name, color, icon, description, is_system, sort_order) VALUES
		($1, 'company',      'Company',      '#3b82f6', 'building',   'Corporations, LLCs, partnerships',     true, 1),
		($1, 'jurisdiction', 'Jurisdiction', '#14b8a6', 'globe',      'Legal jurisdictions, courts',          true, 2),
		($1, 'obligation',   'Obligation',   '#f59e0b', 'file-text',  'Legal duties, requirements',           true, 3),
		($1, 'risk',         'Risk',         '#ef4444', 'alert-triangle','Legal risks, liabilities',           true, 4),
		($1, 'decision',     'Decision',     '#8b5cf6', 'gavel',      'Court decisions, rulings',             true, 5),
		($1, 'contract',     'Contract',     '#22c55e', 'file-check',  'Legal agreements, contracts',         true, 6),
		($1, 'document',     'Document',     '#64748b', 'file',        'Legal documents, filings',            true, 7),
		($1, 'date',         'Date',         '#ec4899', 'calendar',   'Key dates, deadlines, filing dates',   true, 8),
		($1, 'question',     'Question',     '#f97316', 'help-circle','Open legal questions',                 true, 9),
		($1, 'person',       'Person',       '#E85D24', 'user',       'Named individuals, parties',           true, 10)
		ON CONFLICT (agent_id, name) DO NOTHING`, aid)
	if err != nil {
		return fmt.Errorf("seed legal entity types: %w", err)
	}

	// Relation types
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kg_relation_types (agent_id, name, display_name, color, directed, description, is_system, sort_order) VALUES
		($1, 'governs',             'Governs',              '#64748b', true,  'Law or regulation governing an entity', true, 1),
		($1, 'registered_in',       'Registered in',        '#64748b', true,  'Company registration jurisdiction',    true, 2),
		($1, 'issues',              'Issues',               '#64748b', true,  'Authority issuing a decision',         true, 3),
		($1, 'creates_obligation',  'Creates obligation',   '#64748b', true,  'Source creating a legal obligation',   true, 4),
		($1, 'expires_on',          'Expires on',           '#64748b', true,  'Expiration deadline',                  true, 5),
		($1, 'owns',                'Owns',                 '#64748b', true,  'Ownership relationship',               true, 6),
		($1, 'has_role',            'Has role',             '#64748b', true,  'Person role in context',               true, 7),
		($1, 'mitigates',           'Mitigates',            '#64748b', true,  'Risk mitigation',                      true, 8),
		($1, 'answered_by',         'Answered by',          '#64748b', true,  'Question answered by decision/doc',    true, 9),
		($1, 'authored',            'Authored',             '#64748b', true,  'Document authorship',                  true, 10)
		ON CONFLICT (agent_id, name) DO NOTHING`, aid)
	if err != nil {
		return fmt.Errorf("seed legal relation types: %w", err)
	}
	return nil
}

func (s *PGKnowledgeGraphStore) seedDevTypes(ctx context.Context, aid uuid.UUID) error {
	// Entity types
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kg_entity_types (agent_id, name, display_name, color, icon, description, is_system, sort_order) VALUES
		($1, 'project',     'Project',     '#22c55e', 'folder',     'Software projects and initiatives',     true, 1),
		($1, 'component',   'Component',   '#3b82f6', 'package',    'Modules, services, libraries',          true, 2),
		($1, 'dependency',  'Dependency',  '#f97316', 'link',       'External packages and services',        true, 3),
		($1, 'technology',  'Technology',  '#8b5cf6', 'cpu',        'Languages, frameworks, tools',          true, 4),
		($1, 'team',        'Team',        '#E85D24', 'users',      'Development teams and groups',          true, 5),
		($1, 'milestone',   'Milestone',   '#ec4899', 'flag',       'Project milestones and deadlines',      true, 6),
		($1, 'task',        'Task',        '#f59e0b', 'check',      'Work items, issues, tickets',           true, 7),
		($1, 'bug',         'Bug',         '#ef4444', 'bug',        'Defects and issues',                    true, 8)
		ON CONFLICT (agent_id, name) DO NOTHING`, aid)
	if err != nil {
		return fmt.Errorf("seed dev entity types: %w", err)
	}

	// Relation types
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kg_relation_types (agent_id, name, display_name, color, directed, description, is_system, sort_order) VALUES
		($1, 'depends_on',       'Depends on',       '#64748b', true,  'Component or dependency link',   true, 1),
		($1, 'blocks',           'Blocks',           '#64748b', true,  'Blocking issue or dependency',   true, 2),
		($1, 'implements',       'Implements',       '#64748b', true,  'Implementation relationship',    true, 3),
		($1, 'integrates_with',  'Integrates with',  '#64748b', false, 'Integration between components', true, 4),
		($1, 'assigned_to',      'Assigned to',      '#64748b', true,  'Task or component assignment',   true, 5),
		($1, 'created',          'Created',          '#64748b', true,  'Authorship or creation',         true, 6),
		($1, 'completed',        'Completed',        '#64748b', true,  'Completion relationship',        true, 7)
		ON CONFLICT (agent_id, name) DO NOTHING`, aid)
	if err != nil {
		return fmt.Errorf("seed dev relation types: %w", err)
	}
	return nil
}
