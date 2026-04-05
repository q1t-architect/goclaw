package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AgentProvisionTool provides create-only access for agents and teams.
// All creates are rate-limited.
// Disabled by default — admin must enable via builtin_tools settings.
type AgentProvisionTool struct {
	agentStore store.AgentStore
	teamStore  store.TeamStore
	counter    *adminCounter
}

func NewAgentProvisionTool() *AgentProvisionTool {
	return &AgentProvisionTool{
		counter: newAdminCounter(),
	}
}

func (t *AgentProvisionTool) SetAgentStore(s store.AgentStore) { t.agentStore = s }
func (t *AgentProvisionTool) SetTeamStore(s store.TeamStore)   { t.teamStore = s }

func (t *AgentProvisionTool) Name() string { return "agent_provision" }

func (t *AgentProvisionTool) Description() string {
	return "Create new agents and teams. " +
		"Actions: create_agent, create_team. " +
		"All creates are rate-limited. " +
		"Disabled by default — must be enabled by admin."
}

func (t *AgentProvisionTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create_agent", "create_team"},
				"description": "The admin action to perform",
			},
			// Agent params
			"agent_key": map[string]any{
				"type":        "string",
				"description": "Agent key (e.g. 'default', 'daemon'). Required for create_agent.",
			},
			"display_name": map[string]any{
				"type":        "string",
				"description": "Agent display name. Required for create_agent.",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "LLM provider. Required for create_agent.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "LLM model. Required for create_agent.",
			},
			"agent_type": map[string]any{
				"type":        "string",
				"description": "Agent type (open/predefined). Required for create_agent.",
			},
			"context_window": map[string]any{
				"type":        "integer",
				"description": "Context window size. For create_agent.",
			},
			"max_tool_iterations": map[string]any{
				"type":        "integer",
				"description": "Max tool iterations. For create_agent.",
			},
			"workspace_path": map[string]any{
				"type":        "string",
				"description": "Workspace path. For create_agent.",
			},
			// Team params
			"team_name": map[string]any{
				"type":        "string",
				"description": "Team name. Required for create_team.",
			},
			"team_description": map[string]any{
				"type":        "string",
				"description": "Team description. For create_team.",
			},
			"lead_agent_id": map[string]any{
				"type":        "string",
				"description": "Lead agent UUID. Required for create_team.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *AgentProvisionTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.agentStore == nil || t.teamStore == nil {
		return NewResult("Agent provision tool is not enabled for this agent.")
	}

	callingAgentID := store.AgentIDFromContext(ctx)
	if callingAgentID == uuid.Nil {
		return ErrorResult("agent context not available")
	}

	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("action parameter is required")
	}

	switch action {
	case "create_agent":
		return t.handleCreateAgent(ctx, args, callingAgentID)
	case "create_team":
		return t.handleCreateTeam(ctx, args, callingAgentID)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %q", action))
	}
}

func (t *AgentProvisionTool) handleCreateAgent(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}

	agentKey, _ := args["agent_key"].(string)
	displayName, _ := args["display_name"].(string)
	provider, _ := args["provider"].(string)
	model, _ := args["model"].(string)
	agentType, _ := args["agent_type"].(string)

	if agentKey == "" || displayName == "" || provider == "" || model == "" || agentType == "" {
		return ErrorResult("agent_key, display_name, provider, model, and agent_type are required for create_agent")
	}
	if !settings.isAgentTypeAllowed(agentType) {
		return ErrorResult(fmt.Sprintf("agent_type %q not allowed. Configured: %s", agentType, settings.AllowedAgentTypes))
	}

	callerIDStr := callingAgentID.String()
	if !t.counter.canCreate(callerIDStr, settings.MaxAgentCreatesPerHour) {
		return ErrorResult(fmt.Sprintf("agent create limit reached (%d/hour).", settings.MaxAgentCreatesPerHour))
	}

	contextWindow, _ := args["context_window"].(float64)
	maxIter, _ := args["max_tool_iterations"].(float64)
	workspace, _ := args["workspace_path"].(string)

	agent := &store.AgentData{
		AgentKey:          agentKey,
		DisplayName:       displayName,
		Provider:          provider,
		Model:             model,
		AgentType:         agentType,
		ContextWindow:     int(contextWindow),
		MaxToolIterations: int(maxIter),
		Workspace:         workspace,
		Status:            "active",
	}

	if err := t.agentStore.Create(ctx, agent); err != nil {
		slog.Warn("agent_provision.create_agent failed", "error", err, "key", agentKey)
		return ErrorResult(fmt.Sprintf("failed to create agent: %v", err))
	}

	t.counter.incCreate(callerIDStr)
	slog.Info("agent_provision.create_agent", "key", agentKey, "id", agent.ID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Agent created: %s (key: %s, id: %s, type: %s)", displayName, agentKey, agent.ID, agentType))
}

func (t *AgentProvisionTool) handleCreateTeam(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}

	teamName, _ := args["team_name"].(string)
	teamDesc, _ := args["team_description"].(string)
	leadAgentIDStr, _ := args["lead_agent_id"].(string)

	if teamName == "" || leadAgentIDStr == "" {
		return ErrorResult("team_name and lead_agent_id are required for create_team")
	}

	leadAgentID, err := uuid.Parse(leadAgentIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid lead_agent_id: %v", err))
	}

	callerIDStr := callingAgentID.String()
	if !t.counter.canCreate(callerIDStr, settings.MaxTeamCreatesPerHour) {
		return ErrorResult(fmt.Sprintf("team create limit reached (%d/hour).", settings.MaxTeamCreatesPerHour))
	}

	team := &store.TeamData{
		Name:        teamName,
		Description: teamDesc,
		LeadAgentID: leadAgentID,
		Status:      "active",
	}

	if err := t.teamStore.CreateTeam(ctx, team); err != nil {
		slog.Warn("agent_provision.create_team failed", "error", err, "name", teamName)
		return ErrorResult(fmt.Sprintf("failed to create team: %v", err))
	}

	t.counter.incCreate(callerIDStr)
	slog.Info("agent_provision.create_team", "name", teamName, "id", team.ID, "lead", leadAgentID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Team created: %s (id: %s, lead: %s)", teamName, team.ID, leadAgentID))
}
