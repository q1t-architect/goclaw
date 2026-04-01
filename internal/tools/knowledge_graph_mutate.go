package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// kgMutateSettings defines configurable limits for agent KG writes.
type kgMutateSettings struct {
	MaxEntitiesPerRun    int    `json:"max_entities_per_run"`
	MaxRelationsPerRun   int    `json:"max_relations_per_run"`
	AllowedEntityTypes   string `json:"allowed_entity_types"`   // comma-separated, empty = all
	AllowedRelationTypes string `json:"allowed_relation_types"` // comma-separated, empty = all
}

func defaultKGMutateSettings() kgMutateSettings {
	return kgMutateSettings{
		MaxEntitiesPerRun:  10,
		MaxRelationsPerRun: 20,
	}
}

func (t *KnowledgeGraphMutateTool) readSettings(ctx context.Context) kgMutateSettings {
	s := defaultKGMutateSettings()
	if settings := BuiltinToolSettingsFromCtx(ctx); settings != nil {
		if raw, ok := settings["knowledge_graph_mutate"]; ok && len(raw) > 0 {
			_ = json.Unmarshal(raw, &s) // ignore error, use defaults
		}
	}
	return s
}

// isEntityTypeAllowed checks if entity type is in the whitelist (or all allowed if empty).
func (s kgMutateSettings) isEntityTypeAllowed(entityType string) bool {
	if s.AllowedEntityTypes == "" {
		return true
	}
	for _, t := range strings.Split(s.AllowedEntityTypes, ",") {
		if strings.TrimSpace(t) == entityType {
			return true
		}
	}
	return false
}

// isRelationTypeAllowed checks if relation type is in the whitelist (or all allowed if empty).
func (s kgMutateSettings) isRelationTypeAllowed(relType string) bool {
	if s.AllowedRelationTypes == "" {
		return true
	}
	for _, t := range strings.Split(s.AllowedRelationTypes, ",") {
		if strings.TrimSpace(t) == relType {
			return true
		}
	}
	return false
}

// KnowledgeGraphMutateTool provides write access to the knowledge graph for agents.
// It is disabled by default — admin must enable it via builtin_tools settings.
type KnowledgeGraphMutateTool struct {
	kgStore store.KnowledgeGraphStore
}

func NewKnowledgeGraphMutateTool() *KnowledgeGraphMutateTool {
	return &KnowledgeGraphMutateTool{}
}

func (t *KnowledgeGraphMutateTool) SetKGStore(ks store.KnowledgeGraphStore) {
	t.kgStore = ks
}

func (t *KnowledgeGraphMutateTool) Name() string { return "knowledge_graph_mutate" }

func (t *KnowledgeGraphMutateTool) Description() string {
	return "Create or update entities and relations in the knowledge graph. " +
		"Use when you need to record new information about people, projects, or their connections " +
		"that you discover during conversation. " +
		"IMPORTANT: Always search first with knowledge_graph_search to avoid creating duplicates. " +
		"Actions: create_entity, update_entity, create_relation, delete_relation. " +
		"All items created through this tool are tagged as agent-sourced."
}

func (t *KnowledgeGraphMutateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create_entity", "update_entity", "create_relation", "delete_relation"},
				"description": "The mutation action to perform",
			},
			"entity_id": map[string]any{
				"type":        "string",
				"description": "Entity ID (required for update_entity)",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Entity name (required for create_entity)",
			},
			"entity_type": map[string]any{
				"type":        "string",
				"description": "Entity type: person, organization, project, product, technology, task, event, document, concept, location",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Entity or relation description",
			},
			"relation_type": map[string]any{
				"type":        "string",
				"description": "Type of relation (e.g. works_for, manages, related_to)",
			},
			"source_entity_id": map[string]any{
				"type":        "string",
				"description": "Source entity ID for a relation",
			},
			"target_entity_id": map[string]any{
				"type":        "string",
				"description": "Target entity ID for a relation",
			},
			"relation_id": map[string]any{
				"type":        "string",
				"description": "Relation ID (for delete_relation)",
			},
			"confidence": map[string]any{
				"type":        "number",
				"description": "Confidence score 0.0-1.0 (default: 1.0)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *KnowledgeGraphMutateTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.kgStore == nil {
		return NewResult("Knowledge graph is not enabled for this agent.")
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return ErrorResult("agent context not available")
	}
	userID := store.KGUserID(ctx)
	settings := t.readSettings(ctx)

	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("action parameter is required")
	}

	switch action {
	case "create_entity":
		return t.createEntity(ctx, agentID.String(), userID, args, settings)
	case "update_entity":
		return t.updateEntity(ctx, agentID.String(), userID, args, settings)
	case "create_relation":
		return t.createRelation(ctx, agentID.String(), userID, args, settings)
	case "delete_relation":
		return t.deleteRelation(ctx, agentID.String(), userID, args, settings)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %q. Valid: create_entity, update_entity, create_relation, delete_relation", action))
	}
}

func (t *KnowledgeGraphMutateTool) createEntity(ctx context.Context, agentID, userID string, args map[string]any, settings kgMutateSettings) *Result {
	name, _ := args["name"].(string)
	entityType, _ := args["entity_type"].(string)
	desc, _ := args["description"].(string)

	if name == "" || entityType == "" {
		return ErrorResult("name and entity_type are required for create_entity")
	}

	if !settings.isEntityTypeAllowed(entityType) {
		return ErrorResult(fmt.Sprintf("entity type %q is not allowed. Allowed types: %s", entityType, settings.AllowedEntityTypes))
	}

	confidence := 1.0
	if c, ok := args["confidence"].(float64); ok && c > 0 {
		confidence = c
	}

	entity := &store.Entity{
		ID:         uuid.NewString(),
		AgentID:    agentID,
		UserID:     userID,
		ExternalID: fmt.Sprintf("agent:%s", agentID[:8]),
		SourceID:   "agent",
		Name:       name,
		EntityType: entityType,
		Confidence: confidence,
	}
	if desc != "" {
		entity.Description = desc
	}

	if err := t.kgStore.UpsertEntity(ctx, entity); err != nil {
		slog.Warn("kg_mutate.create_entity failed", "error", err)
		return ErrorResult(fmt.Sprintf("failed to create entity: %v", err))
	}

	return NewResult(fmt.Sprintf("Entity created: %s [%s] (id: %s)", name, entityType, entity.ID))
}

func (t *KnowledgeGraphMutateTool) updateEntity(ctx context.Context, agentID, userID string, args map[string]any, _ kgMutateSettings) *Result {
	entityID, _ := args["entity_id"].(string)
	if entityID == "" {
		return ErrorResult("entity_id is required for update_entity")
	}

	updates := map[string]any{}
	if name, ok := args["name"].(string); ok && name != "" {
		updates["name"] = name
	}
	if desc, ok := args["description"].(string); ok {
		updates["description"] = desc
	}
	if entityType, ok := args["entity_type"].(string); ok && entityType != "" {
		updates["entity_type"] = entityType
	}

	if len(updates) == 0 {
		return ErrorResult("no fields to update. Provide name, description, or entity_type")
	}

	entity, err := t.kgStore.UpdateEntity(ctx, agentID, userID, entityID, updates)
	if err != nil {
		slog.Warn("kg_mutate.update_entity failed", "error", err)
		return ErrorResult(fmt.Sprintf("failed to update entity: %v", err))
	}
	if entity == nil {
		return ErrorResult(fmt.Sprintf("entity not found: %s", entityID))
	}

	return NewResult(fmt.Sprintf("Entity updated: %s [%s]", entity.Name, entity.EntityType))
}

func (t *KnowledgeGraphMutateTool) createRelation(ctx context.Context, agentID, userID string, args map[string]any, settings kgMutateSettings) *Result {
	srcID, _ := args["source_entity_id"].(string)
	tgtID, _ := args["target_entity_id"].(string)
	relType, _ := args["relation_type"].(string)

	if srcID == "" || tgtID == "" || relType == "" {
		return ErrorResult("source_entity_id, target_entity_id, and relation_type are required for create_relation")
	}

	if !settings.isRelationTypeAllowed(relType) {
		return ErrorResult(fmt.Sprintf("relation type %q is not allowed. Allowed types: %s", relType, settings.AllowedRelationTypes))
	}

	confidence := 1.0
	if c, ok := args["confidence"].(float64); ok && c > 0 {
		confidence = c
	}

	relation := &store.Relation{
		ID:             uuid.NewString(),
		AgentID:        agentID,
		UserID:         userID,
		SourceEntityID: srcID,
		RelationType:   relType,
		TargetEntityID: tgtID,
		Confidence:     confidence,
		Source:         "agent",
	}

	if err := t.kgStore.UpsertRelation(ctx, relation); err != nil {
		slog.Warn("kg_mutate.create_relation failed", "error", err)
		return ErrorResult(fmt.Sprintf("failed to create relation: %v", err))
	}

	srcName := t.resolveEntityName(ctx, agentID, userID, srcID)
	tgtName := t.resolveEntityName(ctx, agentID, userID, tgtID)

	return NewResult(fmt.Sprintf("Relation created: %s —[%s]→ %s", srcName, strings.ReplaceAll(relType, "_", " "), tgtName))
}

func (t *KnowledgeGraphMutateTool) deleteRelation(ctx context.Context, agentID, userID string, args map[string]any, _ kgMutateSettings) *Result {
	relationID, _ := args["relation_id"].(string)
	if relationID == "" {
		return ErrorResult("relation_id is required for delete_relation")
	}

	if err := t.kgStore.DeleteRelation(ctx, agentID, userID, relationID); err != nil {
		slog.Warn("kg_mutate.delete_relation failed", "error", err)
		return ErrorResult(fmt.Sprintf("failed to delete relation: %v", err))
	}

	return NewResult(fmt.Sprintf("Relation deleted: %s", relationID))
}

// resolveEntityName resolves entity ID to a human-readable name.
func (t *KnowledgeGraphMutateTool) resolveEntityName(ctx context.Context, agentID, userID, entityID string) string {
	e, err := t.kgStore.GetEntity(ctx, agentID, userID, entityID)
	if err == nil && e != nil {
		return e.Name
	}
	if len(entityID) >= 8 {
		return entityID[:8]
	}
	return entityID
}
