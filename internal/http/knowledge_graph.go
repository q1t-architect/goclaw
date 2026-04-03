package http

import (
	"context"
	"net/http"

	kg "github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// KnowledgeGraphHandler handles KG entity/relation management endpoints.
type KnowledgeGraphHandler struct {
	store       store.KnowledgeGraphStore
	providerReg *providers.Registry
}

// NewKnowledgeGraphHandler creates a handler for KG management endpoints.
func NewKnowledgeGraphHandler(s store.KnowledgeGraphStore, providerReg *providers.Registry) *KnowledgeGraphHandler {
	return &KnowledgeGraphHandler{store: s, providerReg: providerReg}
}

// NewExtractor creates an Extractor from the given provider name and model.
func (h *KnowledgeGraphHandler) NewExtractor(ctx context.Context, providerName, model string, minConfidence float64) *kg.Extractor {
	if h.providerReg == nil || providerName == "" || model == "" {
		return nil
	}
	p, err := h.providerReg.Get(ctx, providerName)
	if err != nil {
		return nil
	}
	return kg.NewExtractor(p, model, minConfidence)
}

// RegisterRoutes registers all KG routes on the given mux.
func (h *KnowledgeGraphHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents/{agentID}/kg/entities", h.auth(h.handleListEntities))
	mux.HandleFunc("GET /v1/agents/{agentID}/kg/entities/{entityID}", h.auth(h.handleGetEntity))
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/entities", h.auth(h.handleUpsertEntity))
	mux.HandleFunc("DELETE /v1/agents/{agentID}/kg/entities/{entityID}", h.auth(h.handleDeleteEntity))
	mux.HandleFunc("PATCH /v1/agents/{agentID}/kg/entities/{entityID}", h.auth(h.handleUpdateEntity))

	// Relation CRUD
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/relations", h.auth(h.handleUpsertRelation))
	mux.HandleFunc("DELETE /v1/agents/{agentID}/kg/relations/{relationID}", h.auth(h.handleDeleteRelation))
	mux.HandleFunc("GET /v1/agents/{agentID}/kg/relations", h.auth(h.handleListAllRelations))
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/traverse", h.auth(h.handleTraverse))
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/extract", h.auth(h.handleExtract))
	mux.HandleFunc("GET /v1/agents/{agentID}/kg/stats", h.auth(h.handleStats))
	mux.HandleFunc("GET /v1/agents/{agentID}/kg/graph", h.auth(h.handleGraph))
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/dedup/scan", h.auth(h.handleScanDuplicates))
	mux.HandleFunc("GET /v1/agents/{agentID}/kg/dedup", h.auth(h.handleListDedupCandidates))
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/merge", h.auth(h.handleMergeEntities))
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/dedup/dismiss", h.auth(h.handleDismissCandidate))

	// KG type management
	mux.HandleFunc("GET /v1/agents/{agentID}/kg/entity-types", h.auth(h.handleListEntityTypes))
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/entity-types", h.auth(h.handleCreateEntityType))
	mux.HandleFunc("PATCH /v1/agents/{agentID}/kg/entity-types/{typeID}", h.auth(h.handleUpdateEntityType))
	mux.HandleFunc("DELETE /v1/agents/{agentID}/kg/entity-types/{typeID}", h.auth(h.handleDeleteEntityType))
	mux.HandleFunc("GET /v1/agents/{agentID}/kg/relation-types", h.auth(h.handleListRelationTypes))
	mux.HandleFunc("POST /v1/agents/{agentID}/kg/relation-types", h.auth(h.handleCreateRelationType))
	mux.HandleFunc("PATCH /v1/agents/{agentID}/kg/relation-types/{typeID}", h.auth(h.handleUpdateRelationType))
	mux.HandleFunc("DELETE /v1/agents/{agentID}/kg/relation-types/{typeID}", h.auth(h.handleDeleteRelationType))
}

func (h *KnowledgeGraphHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", func(w http.ResponseWriter, r *http.Request) {
		// KG management endpoints serve the admin UI — use shared KG context
		// so queries don't require exact user_id match. Tenant isolation is
		// still enforced via scopeClause (tenant_id filter).
		ctx := store.WithSharedKG(r.Context())
		next(w, r.WithContext(ctx))
	})
}
