package store

import (
	"context"
	"time"
)

// Entity represents a node in the knowledge graph.
type Entity struct {
	ID          string            `json:"id" db:"id"`
	AgentID     string            `json:"agent_id" db:"agent_id"`
	UserID      string            `json:"user_id,omitempty" db:"user_id"`
	ExternalID  string            `json:"external_id" db:"external_id"`
	Name        string            `json:"name" db:"name"`
	EntityType  string            `json:"entity_type" db:"entity_type"`
	Description string            `json:"description,omitempty" db:"description"`
	Properties  map[string]string `json:"properties,omitempty" db:"properties"`
	SourceID    string            `json:"source_id,omitempty" db:"source_id"`
	Confidence  float64           `json:"confidence" db:"confidence"`
	CreatedAt   int64             `json:"created_at" db:"created_at"`
	UpdatedAt   int64             `json:"updated_at" db:"updated_at"`
	ValidFrom   *time.Time        `json:"valid_from,omitempty" db:"valid_from"`
	ValidUntil  *time.Time        `json:"valid_until,omitempty" db:"valid_until"`
}

// Relation represents an edge between two entities.
type Relation struct {
	ID             string            `json:"id" db:"id"`
	AgentID        string            `json:"agent_id" db:"agent_id"`
	UserID         string            `json:"user_id,omitempty" db:"user_id"`
	SourceEntityID string            `json:"source_entity_id" db:"source_entity_id"`
	RelationType   string            `json:"relation_type" db:"relation_type"`
	TargetEntityID string            `json:"target_entity_id" db:"target_entity_id"`
	Confidence     float64           `json:"confidence" db:"confidence"`
	Properties     map[string]string `json:"properties,omitempty" db:"properties"`
	Source         string            `json:"source,omitempty" db:"source"`
	CreatedAt      int64             `json:"created_at" db:"created_at"`
	UpdatedAt      int64             `json:"updated_at,omitempty" db:"updated_at"`
	ValidFrom      *time.Time        `json:"valid_from,omitempty" db:"valid_from"`
	ValidUntil     *time.Time        `json:"valid_until,omitempty" db:"valid_until"`
}

// TraversalResult is a connected entity with path info.
type TraversalResult struct {
	Entity Entity   `json:"entity" db:"-"`
	Depth  int      `json:"depth" db:"-"`
	Path   []string `json:"path" db:"-"`
	Via    string   `json:"via" db:"-"`
}

// EntityListOptions configures a list query for entities.
type EntityListOptions struct {
	EntityType string
	Limit      int
	Offset     int
}

// GraphStats contains aggregate counts for a scoped graph.
type GraphStats struct {
	EntityCount   int            `json:"entity_count" db:"-"`
	RelationCount int            `json:"relation_count" db:"-"`
	EntityTypes   map[string]int `json:"entity_types" db:"-"`
	UserIDs       []string       `json:"user_ids,omitempty" db:"-"`
}

// DedupCandidate represents a pair of entities that may be duplicates.
type DedupCandidate struct {
	ID         string  `json:"id" db:"id"`
	EntityA    Entity  `json:"entity_a" db:"-"`
	EntityB    Entity  `json:"entity_b" db:"-"`
	Similarity float64 `json:"similarity" db:"similarity"`
	Status     string  `json:"status" db:"status"`
	CreatedAt  int64   `json:"created_at" db:"created_at"`
}

// KnowledgeGraphStore manages entity-relationship graphs.
type KnowledgeGraphStore interface {
	UpsertEntity(ctx context.Context, entity *Entity) error
	GetEntity(ctx context.Context, agentID, userID, entityID string) (*Entity, error)
	UpdateEntity(ctx context.Context, agentID, userID, entityID string, updates map[string]any) (*Entity, error)
	DeleteEntity(ctx context.Context, agentID, userID, entityID string) error
	ListEntities(ctx context.Context, agentID, userID string, opts EntityListOptions) ([]Entity, error)
	SearchEntities(ctx context.Context, agentID, userID, query string, limit int) ([]Entity, error)

	UpsertRelation(ctx context.Context, relation *Relation) error
	DeleteRelation(ctx context.Context, agentID, userID, relationID string) error
	ListRelations(ctx context.Context, agentID, userID, entityID string) ([]Relation, error)
	// ListAllRelations returns all relations for an agent (optionally scoped by user).
	ListAllRelations(ctx context.Context, agentID, userID string, limit int) ([]Relation, error)

	Traverse(ctx context.Context, agentID, userID, startEntityID string, maxDepth int) ([]TraversalResult, error)

	// IngestExtraction upserts entities and relations from an LLM extraction.
	// Returns the DB UUIDs of all upserted entities for downstream processing (e.g. dedup).
	IngestExtraction(ctx context.Context, agentID, userID string, entities []Entity, relations []Relation) ([]string, error)
	PruneByConfidence(ctx context.Context, agentID, userID string, minConfidence float64) (int, error)

	// DedupAfterExtraction checks newly upserted entities for duplicates.
	// Auto-merges at high similarity (>0.98 + name match), flags medium (>0.90) as candidates.
	DedupAfterExtraction(ctx context.Context, agentID, userID string, newEntityIDs []string) (merged int, flagged int, err error)
	// ScanDuplicates scans ALL entities with embeddings for duplicates (self-join).
	// Flags candidates above threshold. Used for on-demand bulk scanning of existing data.
	ScanDuplicates(ctx context.Context, agentID, userID string, threshold float64, limit int) (int, error)
	// ListDedupCandidates returns pending dedup candidates for review.
	ListDedupCandidates(ctx context.Context, agentID, userID string, limit int) ([]DedupCandidate, error)
	// MergeEntities merges sourceID into targetID: re-points relations, deletes source.
	MergeEntities(ctx context.Context, agentID, userID, targetID, sourceID string) error
	// DismissCandidate marks a dedup candidate as dismissed (not a duplicate).
	// Scoped by agentID + tenant to prevent cross-agent dismissal.
	DismissCandidate(ctx context.Context, agentID, candidateID string) error

	Stats(ctx context.Context, agentID, userID string) (*GraphStats, error)

	// Type management
	GetEntityTypes(ctx context.Context, agentID string) ([]EntityType, error)
	UpsertEntityType(ctx context.Context, et *EntityType) error
	DeleteEntityType(ctx context.Context, agentID, typeID string) error
	CountEntitiesByType(ctx context.Context, agentID, typeID string) (int64, error)

	GetRelationTypes(ctx context.Context, agentID string) ([]RelationType, error)
	UpsertRelationType(ctx context.Context, rt *RelationType) error
	DeleteRelationType(ctx context.Context, agentID, typeID string) error
	CountRelationsByType(ctx context.Context, agentID, typeID string) (int64, error)

	// SeedKGTypes seeds default types for an agent from a preset.
	// Presets: "general" (10+22 default types), "legal", "development", "empty" (no-op).
	SeedKGTypes(ctx context.Context, agentID string, preset string) error

	// Temporal queries (v3)
	ListEntitiesTemporal(ctx context.Context, agentID, userID string, opts EntityListOptions, temporal TemporalQueryOptions) ([]Entity, error)
	SupersedeEntity(ctx context.Context, old *Entity, replacement *Entity) error

	// SetEmbeddingProvider configures the embedding provider for semantic search.
	SetEmbeddingProvider(provider EmbeddingProvider)

	Close() error
}

// PropertyField describes one custom field in a type's properties_schema.
type PropertyField struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Type       string   `json:"type"` // "string", "number", "date", "enum"
	Required   bool     `json:"required"`
	EnumValues []string `json:"enum_values,omitempty"`
}

// EntityType represents a custom entity type definition.
type EntityType struct {
	ID               string           `json:"id"`
	AgentID          string           `json:"agent_id"`
	Name             string           `json:"name"`              // slug: "person", "contract"
	DisplayName      string           `json:"display_name"`      // "Person", "Contract"
	Color            string           `json:"color"`             // "#E85D24"
	Icon             string           `json:"icon,omitempty"`    // "user", "file"
	Description      string           `json:"description"`       // Description for LLM prompt
	PropertiesSchema []PropertyField  `json:"properties_schema,omitempty"`
	IsSystem         bool             `json:"is_system"`
	SortOrder        int              `json:"sort_order"`
	CreatedAt        int64            `json:"created_at"`
	UpdatedAt        int64            `json:"updated_at"`
}

// RelationType represents a custom relation type definition.
type RelationType struct {
	ID               string           `json:"id"`
	AgentID          string           `json:"agent_id"`
	Name             string           `json:"name"`              // "works_on", "governs"
	DisplayName      string           `json:"display_name"`      // "Works on"
	Color            string           `json:"color"`
	Directed         bool             `json:"directed"`
	Description      string           `json:"description"`
	PropertiesSchema []PropertyField  `json:"properties_schema,omitempty"`
	IsSystem         bool             `json:"is_system"`
	SortOrder        int              `json:"sort_order"`
	CreatedAt        int64            `json:"created_at"`
	UpdatedAt        int64            `json:"updated_at"`
}

// KGTypePreset defines a named preset of entity + relation types.
type KGTypePreset struct {
	Name          string        `json:"name"`
	DisplayName   string        `json:"display_name"`
	EntityTypes   []EntityType  `json:"entity_types"`
	RelationTypes []RelationType `json:"relation_types"`
}
