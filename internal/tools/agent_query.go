package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AgentQueryTool provides global read-only access to agents, teams, and skill grants.
// No filtering — can see everything. For admin-level inspection.
// Disabled by default — admin must enable via builtin_tools settings.
type AgentQueryTool struct {
	agentStore store.AgentStore
	teamStore  store.TeamStore
	skillStore store.SkillManageStore
}

func NewAgentQueryTool() *AgentQueryTool { return &AgentQueryTool{} }

func (t *AgentQueryTool) SetAgentStore(s store.AgentStore)      { t.agentStore = s }
func (t *AgentQueryTool) SetTeamStore(s store.TeamStore)        { t.teamStore = s }
func (t *AgentQueryTool) SetSkillManageStore(s store.SkillManageStore) { t.skillStore = s }

func (t *AgentQueryTool) Name() string { return "agent_query" }

func (t *AgentQueryTool) Description() string {
	return "Query agents, teams, and skill grants (read-only, global access). " +
		"Actions: list_agents, get_agent, get_context_file, list_teams, get_team, list_skill_grants. " +
		"IMPORTANT: Cannot modify your own agent configuration (self-modification guard). " +
		"Disabled by default — must be enabled by admin."
}

func (t *AgentQueryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list_agents", "get_agent", "get_context_file", "list_teams", "get_team", "list_skill_grants"},
				"description": "The admin action to perform",
			},
			"agent_key": map[string]any{
				"type":        "string",
				"description": "Agent key (e.g. 'default', 'daemon'). Used for get_agent.",
			},
			"agent_id": map[string]any{
				"type":        "string",
				"description": "Agent UUID. Alternative to agent_key for get_agent.",
			},
			"owner_id": map[string]any{
				"type":        "string",
				"description": "Filter agents by owner ID (for list_agents). Empty = all agents.",
			},
			"file_name": map[string]any{
				"type":        "string",
				"description": "Context file name (e.g. 'SOUL.md', 'IDENTITY.md', 'AGENTS.md'). Required for get/set_context_file.",
			},
			"team_id": map[string]any{
				"type":        "string",
				"description": "Team UUID. Required for get_team.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *AgentQueryTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.agentStore == nil || t.teamStore == nil {
		return NewResult("Agent query tool is not enabled for this agent.")
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
	case "list_agents":
		return t.handleListAgents(ctx, args)
	case "get_agent":
		return t.handleGetAgent(ctx, args, callingAgentID)
	case "get_context_file":
		return t.handleGetContextFile(ctx, args, callingAgentID)
	case "list_teams":
		return t.handleListTeams(ctx)
	case "get_team":
		return t.handleGetTeam(ctx, args)
	case "list_skill_grants":
		return t.handleListSkillGrants(ctx, args, callingAgentID)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %q", action))
	}
}

func (t *AgentQueryTool) handleListAgents(ctx context.Context, args map[string]any) *Result {
	ownerID, _ := args["owner_id"].(string)
	agents, err := t.agentStore.List(ctx, ownerID)
	if err != nil {
		slog.Warn("agent_query.list_agents failed", "error", err)
		return ErrorResult(fmt.Sprintf("failed to list agents: %v", err))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d agents:\n", len(agents)))
	for _, a := range agents {
		status := a.Status
		if status == "" {
			status = "active"
		}
		sb.WriteString(fmt.Sprintf("- %s (key: %s, id: %s, type: %s, status: %s)\n",
			a.DisplayName, a.AgentKey, a.ID, a.AgentType, status))
	}
	return NewResult(sb.String())
}

func (t *AgentQueryTool) handleGetAgent(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	agentKey, _ := args["agent_key"].(string)
	agentIDStr, _ := args["agent_id"].(string)

	var agent *store.AgentData
	var err error

	if agentIDStr != "" {
		agentUUID, parseErr := uuid.Parse(agentIDStr)
		if parseErr != nil {
			return ErrorResult(fmt.Sprintf("invalid agent_id: %v", parseErr))
		}
		agent, err = t.agentStore.GetByID(ctx, agentUUID)
	} else if agentKey != "" {
		agent, err = t.agentStore.GetByKey(ctx, agentKey)
	} else {
		return ErrorResult("agent_key or agent_id is required for get_agent")
	}

	if err != nil || agent == nil {
		return ErrorResult(fmt.Sprintf("agent not found: %s", agentKey+agentIDStr))
	}

	selfModify := agent.ID == callingAgentID

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Agent: %s\n", agent.DisplayName))
	sb.WriteString(fmt.Sprintf("Key: %s\n", agent.AgentKey))
	sb.WriteString(fmt.Sprintf("ID: %s\n", agent.ID))
	sb.WriteString(fmt.Sprintf("Type: %s\n", agent.AgentType))
	sb.WriteString(fmt.Sprintf("Status: %s\n", agent.Status))
	sb.WriteString(fmt.Sprintf("Provider: %s\n", agent.Provider))
	sb.WriteString(fmt.Sprintf("Model: %s\n", agent.Model))
	sb.WriteString(fmt.Sprintf("Workspace: %s\n", agent.Workspace))
	sb.WriteString(fmt.Sprintf("ContextWindow: %d\n", agent.ContextWindow))
	sb.WriteString(fmt.Sprintf("MaxToolIterations: %d\n", agent.MaxToolIterations))
	if agent.IsDefault {
		sb.WriteString("IsDefault: true\n")
	}
	if selfModify {
		sb.WriteString("\n⚠️ This is YOUR agent. Self-modification is not allowed via this tool.\n")
	}
	return NewResult(sb.String())
}

func (t *AgentQueryTool) handleGetContextFile(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	agentKey, _ := args["agent_key"].(string)
	agentIDStr, _ := args["agent_id"].(string)
	fileName, _ := args["file_name"].(string)

	if fileName == "" {
		return ErrorResult("file_name is required for get_context_file")
	}
	if !isAllowedContextFile(fileName) {
		return ErrorResult(fmt.Sprintf("file %q is not a valid context file. Allowed: %s", fileName, strings.Join(allowedContextFiles, ", ")))
	}

	agentID, err := resolveAgentID(ctx, t.agentStore, agentKey, agentIDStr)
	if err != nil {
		return ErrorResult(err.Error())
	}

	files, err := t.agentStore.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		slog.Warn("agent_query.get_context_file failed", "error", err)
		return ErrorResult(fmt.Sprintf("failed to get context files: %v", err))
	}

	for _, f := range files {
		if f.FileName == fileName {
			selfFlag := ""
			if agentID == callingAgentID {
				selfFlag = "\n⚠️ This is YOUR agent's file. Writing to it is not allowed."
			}
			return NewResult(fmt.Sprintf("=== %s (agent: %s) ===\n%s%s", fileName, agentID, f.Content, selfFlag))
		}
	}

	return ErrorResult(fmt.Sprintf("file %q not found for agent %s", fileName, agentID))
}

func (t *AgentQueryTool) handleListTeams(ctx context.Context) *Result {
	teams, err := t.teamStore.ListTeams(ctx)
	if err != nil {
		slog.Warn("agent_query.list_teams failed", "error", err)
		return ErrorResult(fmt.Sprintf("failed to list teams: %v", err))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d teams:\n", len(teams)))
	for _, tm := range teams {
		sb.WriteString(fmt.Sprintf("- %s (id: %s, lead: %s [%s], members: %d, status: %s)\n",
			tm.Name, tm.ID, tm.LeadAgentKey, tm.LeadDisplayName, tm.MemberCount, tm.Status))
	}
	return NewResult(sb.String())
}

func (t *AgentQueryTool) handleGetTeam(ctx context.Context, args map[string]any) *Result {
	teamIDStr, _ := args["team_id"].(string)
	if teamIDStr == "" {
		return ErrorResult("team_id is required for get_team")
	}

	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid team_id: %v", err))
	}

	team, err := t.teamStore.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		return ErrorResult(fmt.Sprintf("team not found: %s", teamIDStr))
	}

	members, _ := t.teamStore.ListMembers(ctx, teamID)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Team: %s\n", team.Name))
	sb.WriteString(fmt.Sprintf("ID: %s\n", team.ID))
	sb.WriteString(fmt.Sprintf("Status: %s\n", team.Status))
	sb.WriteString(fmt.Sprintf("Lead: %s (%s)\n", team.LeadDisplayName, team.LeadAgentKey))
	if team.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n", team.Description))
	}
	sb.WriteString(fmt.Sprintf("\nMembers (%d):\n", len(members)))
	for _, m := range members {
		sb.WriteString(fmt.Sprintf("- %s (key: %s, role: %s)\n", m.DisplayName, m.AgentKey, m.Role))
	}
	return NewResult(sb.String())
}

func (t *AgentQueryTool) handleListSkillGrants(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	if t.skillStore == nil {
		return ErrorResult("skill management is not available (skill store not wired)")
	}
	targetAgentID, err := resolveAgentID(ctx, t.agentStore, strArg(args, "agent_key"), strArg(args, "agent_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	skills, err := t.skillStore.ListWithGrantStatus(ctx, targetAgentID)
	if err != nil {
		slog.Warn("agent_query.list_skill_grants failed", "error", err, "agent", targetAgentID)
		return ErrorResult(fmt.Sprintf("failed to list skill grants: %v", err))
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Skills for agent %s (%d total):\n", targetAgentID, len(skills)))
	for _, s := range skills {
		status := "not granted"
		if s.Granted {
			status = "granted"
		}
		sb.WriteString(fmt.Sprintf("- %s (slug: %s, %s, system: %v)\n", s.Name, s.Slug, status, s.IsSystem))
	}
	return NewResult(sb.String())
}
