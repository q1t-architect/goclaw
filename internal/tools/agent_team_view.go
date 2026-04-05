package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AgentTeamViewTool provides team-scoped read-only access to agents.
// An agent using this tool can only see agents that are members of teams it leads.
// Disabled by default — admin must enable via builtin_tools settings.
type AgentTeamViewTool struct {
	agentStore store.AgentStore
	teamStore  store.TeamStore
}

func NewAgentTeamViewTool() *AgentTeamViewTool {
	return &AgentTeamViewTool{}
}

func (t *AgentTeamViewTool) SetAgentStore(s store.AgentStore)  { t.agentStore = s }
func (t *AgentTeamViewTool) SetTeamStore(s store.TeamStore)    { t.teamStore = s }

func (t *AgentTeamViewTool) Name() string { return "agent_team_view" }

func (t *AgentTeamViewTool) Description() string {
	return "View agents and context files for teams you lead. " +
		"Actions: list_agents, get_agent, get_context_file. " +
		"Only shows agents that are members of teams where you are the lead. " +
		"Disabled by default — must be enabled by admin."
}

func (t *AgentTeamViewTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list_agents", "get_agent", "get_context_file"},
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
				"description": "Context file name (e.g. 'SOUL.md', 'IDENTITY.md', 'AGENTS.md'). Required for get_context_file.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *AgentTeamViewTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.agentStore == nil || t.teamStore == nil {
		return NewResult("Agent team view tool is not enabled for this agent.")
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
		return t.handleListAgents(ctx, args, callingAgentID)
	case "get_agent":
		return t.handleGetAgent(ctx, args, callingAgentID)
	case "get_context_file":
		return t.handleGetContextFile(ctx, args, callingAgentID)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %q", action))
	}
}

func (t *AgentTeamViewTool) handleListAgents(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	visible, err := teamMemberIDsForLead(ctx, t.teamStore, callingAgentID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to determine team membership: %v", err))
	}

	ownerID, _ := args["owner_id"].(string)
	agents, err := t.agentStore.List(ctx, ownerID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list agents: %v", err))
	}

	var sb strings.Builder
	count := 0
	for _, a := range agents {
		if !visible[a.ID] {
			continue
		}
		count++
		status := a.Status
		if status == "" {
			status = "active"
		}
		sb.WriteString(fmt.Sprintf("- %s (key: %s, id: %s, type: %s, status: %s)\n",
			a.DisplayName, a.AgentKey, a.ID, a.AgentType, status))
	}
	if count == 0 {
		sb.WriteString("No agents found in your teams.\n")
	} else {
		sb.Reset()
		sb.WriteString(fmt.Sprintf("Found %d agents in your teams:\n", count))
		for _, a := range agents {
			if !visible[a.ID] {
				continue
			}
			status := a.Status
			if status == "" {
				status = "active"
			}
			sb.WriteString(fmt.Sprintf("- %s (key: %s, id: %s, type: %s, status: %s)\n",
				a.DisplayName, a.AgentKey, a.ID, a.AgentType, status))
		}
	}
	return NewResult(sb.String())
}

func (t *AgentTeamViewTool) handleGetAgent(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
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

	// Check team-scoped visibility
	visible, err := teamMemberIDsForLead(ctx, t.teamStore, callingAgentID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to determine team membership: %v", err))
	}
	if !visible[agent.ID] {
		return ErrorResult(fmt.Sprintf("agent %s is not in any team you lead", agent.AgentKey))
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

func (t *AgentTeamViewTool) handleGetContextFile(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
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

	// Check team-scoped visibility
	visible, err := teamMemberIDsForLead(ctx, t.teamStore, callingAgentID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to determine team membership: %v", err))
	}
	if !visible[agentID] {
		return ErrorResult("you can only read context files for agents in teams you lead")
	}

	files, err := t.agentStore.GetAgentContextFiles(ctx, agentID)
	if err != nil {
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
