package tools

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestKGMutate_CreateEntity(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action":      "create_entity",
		"name":        "TestProject",
		"entity_type": "project",
		"description": "A test project",
	})

	if result.IsError {
		t.Errorf("expected success, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "TestProject") {
		t.Errorf("expected result to contain 'TestProject', got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "project") {
		t.Errorf("expected result to contain 'project', got: %s", result.ForLLM)
	}
}

func TestKGMutate_CreateEntity_MissingFields(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action": "create_entity",
		"name":   "TestProject",
		// missing entity_type
	})

	if !result.IsError {
		t.Error("expected error for missing entity_type")
	}
}

func TestKGMutate_UpdateEntity(t *testing.T) {
	ms := newMockKGStore()
	entityID := uuid.NewString()
	ms.entities[entityID] = store.Entity{
		ID: entityID, AgentID: testAgentID.String(), UserID: testUserID,
		Name: "OldName", EntityType: "person",
	}

	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action":      "update_entity",
		"entity_id":   entityID,
		"name":        "NewName",
		"description": "Updated description",
	})

	if result.IsError {
		t.Errorf("expected success, got error: %s", result.ForLLM)
	}
}

func TestKGMutate_UpdateEntity_MissingID(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action": "update_entity",
		"name":   "NewName",
	})

	if !result.IsError {
		t.Error("expected error for missing entity_id")
	}
}

func TestKGMutate_UpdateEntity_NoFields(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action":    "update_entity",
		"entity_id": "some-id",
	})

	if !result.IsError {
		t.Error("expected error for no fields to update")
	}
}

func TestKGMutate_CreateRelation(t *testing.T) {
	ms := newMockKGStore()
	srcID := uuid.NewString()
	tgtID := uuid.NewString()
	ms.entities[srcID] = store.Entity{ID: srcID, Name: "Alice", EntityType: "person"}
	ms.entities[tgtID] = store.Entity{ID: tgtID, Name: "Acme Corp", EntityType: "organization"}

	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action":            "create_relation",
		"source_entity_id":  srcID,
		"target_entity_id":  tgtID,
		"relation_type":     "works_for",
	})

	if result.IsError {
		t.Errorf("expected success, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "works for") {
		t.Errorf("expected relation type in output, got: %s", result.ForLLM)
	}
	// Verify source is 'agent'
	if len(ms.relations) != 1 || ms.relations[0].Source != "agent" {
		t.Errorf("expected relation with source='agent', got: %+v", ms.relations)
	}
}

func TestKGMutate_CreateRelation_MissingFields(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action":           "create_relation",
		"source_entity_id": "src",
		// missing target_entity_id and relation_type
	})

	if !result.IsError {
		t.Error("expected error for missing fields")
	}
}

func TestKGMutate_DeleteRelation(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action":      "delete_relation",
		"relation_id": "some-relation-id",
	})

	if result.IsError {
		t.Errorf("expected success (mock always succeeds), got error: %s", result.ForLLM)
	}
}

func TestKGMutate_DeleteRelation_MissingID(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action": "delete_relation",
	})

	if !result.IsError {
		t.Error("expected error for missing relation_id")
	}
}

func TestKGMutate_UnknownAction(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action": "merge_entities",
	})

	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestKGMutate_MissingAction(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{})

	if !result.IsError {
		t.Error("expected error for missing action")
	}
}

func TestKGMutate_NilStore(t *testing.T) {
	tool := NewKnowledgeGraphMutateTool()
	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{"action": "create_entity"})

	if result.IsError {
		t.Error("nil store should return friendly message, not error")
	}
	if !strings.Contains(result.ForLLM, "not enabled") {
		t.Errorf("expected 'not enabled' message, got: %s", result.ForLLM)
	}
}

func TestKGMutate_CreateRelation_SetsSourceAgent(t *testing.T) {
	ms := newMockKGStore()
	srcID := uuid.NewString()
	tgtID := uuid.NewString()
	ms.entities[srcID] = store.Entity{ID: srcID, Name: "X", EntityType: "concept"}
	ms.entities[tgtID] = store.Entity{ID: tgtID, Name: "Y", EntityType: "concept"}

	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action":            "create_relation",
		"source_entity_id":  srcID,
		"target_entity_id":  tgtID,
		"relation_type":     "related_to",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(ms.relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(ms.relations))
	}
	if ms.relations[0].Source != "agent" {
		t.Errorf("expected source='agent', got source='%s'", ms.relations[0].Source)
	}
}

func TestKGMutate_CreateEntity_WithConfidence(t *testing.T) {
	ms := newMockKGStore()
	tool := NewKnowledgeGraphMutateTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.Execute(ctx, map[string]any{
		"action":      "create_entity",
		"name":        "UncertainThing",
		"entity_type": "concept",
		"confidence":  0.5,
	})

	if result.IsError {
		t.Errorf("expected success, got error: %s", result.ForLLM)
	}
	// Find the entity in mock store
	for _, e := range ms.entities {
		if e.Name == "UncertainThing" {
			if e.Confidence != 0.5 {
				t.Errorf("expected confidence 0.5, got %f", e.Confidence)
			}
			return
		}
	}
	t.Error("entity not found in mock store")
}
