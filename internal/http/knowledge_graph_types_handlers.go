package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// --- Entity Types ---

func (h *KnowledgeGraphHandler) handleListEntityTypes(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	types, err := h.store.GetEntityTypes(r.Context(), agentID)
	if err != nil {
		slog.Warn("kg.list_entity_types failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if types == nil {
		types = []store.EntityType{}
	}
	writeJSON(w, http.StatusOK, types)
}

func (h *KnowledgeGraphHandler) handleCreateEntityType(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")

	var body struct {
		Name             string                `json:"name"`
		DisplayName      string                `json:"display_name"`
		Color            string                `json:"color"`
		Icon             string                `json:"icon"`
		Description      string                `json:"description"`
		PropertiesSchema []store.PropertyField `json:"properties_schema"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	et := store.EntityType{
		AgentID:          agentID,
		Name:             body.Name,
		DisplayName:      body.DisplayName,
		Color:            body.Color,
		Icon:             body.Icon,
		Description:      body.Description,
		PropertiesSchema: body.PropertiesSchema,
	}

	if err := h.store.UpsertEntityType(r.Context(), &et); err != nil {
		slog.Warn("kg.create_entity_type failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, et)
}

func (h *KnowledgeGraphHandler) handleUpdateEntityType(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")
	typeID := r.PathValue("typeID")

	types, err := h.store.GetEntityTypes(r.Context(), agentID)
	if err != nil {
		slog.Warn("kg.update_entity_type: list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var existing *store.EntityType
	for i := range types {
		if types[i].ID == typeID {
			existing = &types[i]
			break
		}
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "entity type not found"})
		return
	}

	var body struct {
		Name             *string                `json:"name"`
		DisplayName      *string                `json:"display_name"`
		Color            *string                `json:"color"`
		Icon             *string                `json:"icon"`
		Description      *string                `json:"description"`
		PropertiesSchema *[]store.PropertyField `json:"properties_schema"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if body.Name != nil {
		existing.Name = *body.Name
	}
	if body.DisplayName != nil {
		existing.DisplayName = *body.DisplayName
	}
	if body.Color != nil {
		existing.Color = *body.Color
	}
	if body.Icon != nil {
		existing.Icon = *body.Icon
	}
	if body.Description != nil {
		existing.Description = *body.Description
	}
	if body.PropertiesSchema != nil {
		existing.PropertiesSchema = *body.PropertiesSchema
	}

	if err := h.store.UpsertEntityType(r.Context(), existing); err != nil {
		slog.Warn("kg.update_entity_type failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *KnowledgeGraphHandler) handleDeleteEntityType(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	typeID := r.PathValue("typeID")

	err := h.store.DeleteEntityType(r.Context(), agentID, typeID)
	if err != nil {
		slog.Warn("kg.delete_entity_type failed", "error", err)
		status := http.StatusInternalServerError
		switch {
		case err.Error() == "cannot delete system entity type":
			status = http.StatusForbidden
		case errors.Is(err, pg.ErrEntityTypeInUse):
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Relation Types ---

func (h *KnowledgeGraphHandler) handleListRelationTypes(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	types, err := h.store.GetRelationTypes(r.Context(), agentID)
	if err != nil {
		slog.Warn("kg.list_relation_types failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if types == nil {
		types = []store.RelationType{}
	}
	writeJSON(w, http.StatusOK, types)
}

func (h *KnowledgeGraphHandler) handleCreateRelationType(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")

	var body struct {
		Name             string                `json:"name"`
		DisplayName      string                `json:"display_name"`
		Color            string                `json:"color"`
		Description      string                `json:"description"`
		Directed         bool                  `json:"directed"`
		PropertiesSchema []store.PropertyField `json:"properties_schema"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	rt := store.RelationType{
		AgentID:          agentID,
		Name:             body.Name,
		DisplayName:      body.DisplayName,
		Color:            body.Color,
		Description:      body.Description,
		Directed:         body.Directed,
		PropertiesSchema: body.PropertiesSchema,
	}

	if err := h.store.UpsertRelationType(r.Context(), &rt); err != nil {
		slog.Warn("kg.create_relation_type failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

func (h *KnowledgeGraphHandler) handleUpdateRelationType(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")
	typeID := r.PathValue("typeID")

	types, err := h.store.GetRelationTypes(r.Context(), agentID)
	if err != nil {
		slog.Warn("kg.update_relation_type: list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var existing *store.RelationType
	for i := range types {
		if types[i].ID == typeID {
			existing = &types[i]
			break
		}
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "relation type not found"})
		return
	}

	var body struct {
		Name             *string                `json:"name"`
		DisplayName      *string                `json:"display_name"`
		Color            *string                `json:"color"`
		Description      *string                `json:"description"`
		Directed         *bool                  `json:"directed"`
		PropertiesSchema *[]store.PropertyField `json:"properties_schema"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if body.Name != nil {
		existing.Name = *body.Name
	}
	if body.DisplayName != nil {
		existing.DisplayName = *body.DisplayName
	}
	if body.Color != nil {
		existing.Color = *body.Color
	}
	if body.Description != nil {
		existing.Description = *body.Description
	}
	if body.Directed != nil {
		existing.Directed = *body.Directed
	}
	if body.PropertiesSchema != nil {
		existing.PropertiesSchema = *body.PropertiesSchema
	}

	if err := h.store.UpsertRelationType(r.Context(), existing); err != nil {
		slog.Warn("kg.update_relation_type failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *KnowledgeGraphHandler) handleDeleteRelationType(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	typeID := r.PathValue("typeID")

	err := h.store.DeleteRelationType(r.Context(), agentID, typeID)
	if err != nil {
		slog.Warn("kg.delete_relation_type failed", "error", err)
		status := http.StatusInternalServerError
		switch {
		case err.Error() == "cannot delete system relation type":
			status = http.StatusForbidden
		case errors.Is(err, pg.ErrRelationTypeInUse):
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
