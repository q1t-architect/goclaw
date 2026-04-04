package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// agentAdminSettings defines configurable limits for agent admin operations.
type agentAdminSettings struct {
	MaxFileWritesPerHour int    `json:"max_file_writes_per_hour"` // default: 20
	AllowedFileTypes     string `json:"allowed_file_types"`       // comma-separated, empty = all 8 files
	ProtectedAgentKeys   string `json:"protected_agent_keys"`     // comma-separated agent keys that cannot be modified
	ReadOnly             bool   `json:"read_only"`                // if true, disables all write operations
}

func defaultAgentAdminSettings() agentAdminSettings {
	return agentAdminSettings{
		MaxFileWritesPerHour: 20,
	}
}

func (t *AgentAdminTool) readSettings(ctx context.Context) agentAdminSettings {
	s := defaultAgentAdminSettings()
	if settings := BuiltinToolSettingsFromCtx(ctx); settings != nil {
		if raw, ok := settings["agent_admin"]; ok && len(raw) > 0 {
			_ = json.Unmarshal(raw, &s) // ignore error, use defaults
		}
	}
	return s
}

// allowedContextFiles is the list of context files the tool can read/write.
// Mirrors allowedAgentFiles from gateway/methods/agents_files.go.
var allowedContextFiles = []string{
	bootstrap.AgentsFile,         // "AGENTS.md"
	bootstrap.SoulFile,           // "SOUL.md"
	bootstrap.IdentityFile,       // "IDENTITY.md"
	bootstrap.UserFile,           // "USER.md"
	bootstrap.UserPredefinedFile, // "USER_PREDEFINED.md"
	bootstrap.BootstrapFile,      // "BOOTSTRAP.md"
	bootstrap.MemoryJSONFile,     // "MEMORY.json"
	bootstrap.HeartbeatFile,      // "HEARTBEAT.md"
}

func isAllowedContextFile(name string) bool {
	for _, f := range allowedContextFiles {
		if f == name {
			return true
		}
	}
	return false
}

// isFileTypeAllowed checks settings whitelist. If empty, all 8 are allowed.
func (s agentAdminSettings) isFileTypeAllowed(fileName string) bool {
	if s.AllowedFileTypes == "" {
		return isAllowedContextFile(fileName) // still must be in the 8
	}
	for _, t := range strings.Split(s.AllowedFileTypes, ",") {
		t = strings.TrimSpace(t)
		if t == fileName {
			return isAllowedContextFile(fileName)
		}
	}
	return false
}

// isProtectedAgent checks if agent key is in the protected list.
func (s agentAdminSettings) isProtectedAgent(agentKey string) bool {
	if s.ProtectedAgentKeys == "" {
		return false
	}
	for _, k := range strings.Split(s.ProtectedAgentKeys, ",") {
		if strings.TrimSpace(k) == agentKey {
			return true
		}
	}
	return false
}

// adminCounter tracks write operations per agent within a time window.
type adminCounter struct {
	mu       sync.Mutex
	counters map[string]*adminRunCount
}

type adminRunCount struct {
	writes   int
	lastSeen time.Time
}

func newAdminCounter() *adminCounter {
	c := &adminCounter{counters: make(map[string]*adminRunCount)}
	go c.cleanup()
	return c
}

func (c *adminCounter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		for k, v := range c.counters {
			if time.Since(v.lastSeen) > 10*time.Minute {
				delete(c.counters, k)
			}
		}
		c.mu.Unlock()
	}
}

func (c *adminCounter) canWrite(agentID string, limit int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(agentID).writes < limit
}

func (c *adminCounter) incWrite(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.get(agentID)
	r.writes++
	r.lastSeen = time.Now()
}

func (c *adminCounter) get(agentID string) *adminRunCount {
	r, ok := c.counters[agentID]
	if !ok {
		r = &adminRunCount{}
		c.counters[agentID] = r
	}
	return r
}

// AgentAdminTool provides read access to agent/team configs and write access to context files.
// Disabled by default — admin must enable via builtin_tools settings.
type AgentAdminTool struct {
	agentStore store.AgentStore
	teamStore  store.TeamStore
	counter    *adminCounter
}

func NewAgentAdminTool() *AgentAdminTool {
	return &AgentAdminTool{
		counter: newAdminCounter(),
	}
}

func (t *AgentAdminTool) SetAgentStore(s store.AgentStore) {
	t.agentStore = s
}

func (t *AgentAdminTool) SetTeamStore(s store.TeamStore) {
	t.teamStore = s
}

func (t *AgentAdminTool) Name() string { return "agent_admin" }

func (t *AgentAdminTool) Description() string {
	return "Manage agents and teams: read configurations, list agents/teams, read and write context files " +
		"(SOUL.md, IDENTITY.md, AGENTS.md, USER.md, etc). " +
		"Actions: list_agents, get_agent, get_context_file, set_context_file, list_teams, get_team. " +
		"IMPORTANT: Cannot modify your own agent configuration (self-modification guard). " +
		"Disabled by default — must be enabled by admin."
}

func (t *AgentAdminTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list_agents", "get_agent", "get_context_file", "set_context_file", "list_teams", "get_team"},
				"description": "The admin action to perform",
			},
			// Agent params
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
			// Context file params
			"file_name": map[string]any{
				"type":        "string",
				"description": "Context file name (e.g. 'SOUL.md', 'IDENTITY.md', 'AGENTS.md'). Required for get/set_context_file.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "File content to write. Required for set_context_file.",
			},
			"propagate": map[string]any{
				"type":        "boolean",
				"description": "If true, propagate the file to all user instances (set_context_file only). Default: false.",
			},
			// Team params
			"team_id": map[string]any{
				"type":        "string",
				"description": "Team UUID. Required for get_team.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *AgentAdminTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.agentStore == nil || t.teamStore == nil {
		return NewResult("Agent admin tool is not enabled for this agent.")
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
	case "set_context_file":
		return t.handleSetContextFile(ctx, args, callingAgentID)
	case "list_teams":
		return t.handleListTeams(ctx)
	case "get_team":
		return t.handleGetTeam(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %q. Valid: list_agents, get_agent, get_context_file, set_context_file, list_teams, get_team", action))
	}
}

// handleListAgents returns all agents (optionally filtered by owner).
func (t *AgentAdminTool) handleListAgents(ctx context.Context, args map[string]any) *Result {
	ownerID, _ := args["owner_id"].(string)

	agents, err := t.agentStore.List(ctx, ownerID)
	if err != nil {
		slog.Warn("agent_admin.list_agents failed", "error", err)
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

// handleGetAgent returns detailed info for a single agent.
func (t *AgentAdminTool) handleGetAgent(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
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

// handleGetContextFile reads a context file for a target agent.
func (t *AgentAdminTool) handleGetContextFile(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	agentKey, _ := args["agent_key"].(string)
	agentIDStr, _ := args["agent_id"].(string)
	fileName, _ := args["file_name"].(string)

	if fileName == "" {
		return ErrorResult("file_name is required for get_context_file")
	}
	if !isAllowedContextFile(fileName) {
		return ErrorResult(fmt.Sprintf("file %q is not a valid context file. Allowed: %s", fileName, strings.Join(allowedContextFiles, ", ")))
	}

	agentID, err := t.resolveAgentID(ctx, agentKey, agentIDStr)
	if err != nil {
		return ErrorResult(err.Error())
	}

	files, err := t.agentStore.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		slog.Warn("agent_admin.get_context_file failed", "error", err)
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

// handleSetContextFile writes a context file for a target agent.
func (t *AgentAdminTool) handleSetContextFile(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)

	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode. set_context_file is disabled.")
	}

	agentKey, _ := args["agent_key"].(string)
	agentIDStr, _ := args["agent_id"].(string)
	fileName, _ := args["file_name"].(string)
	content, _ := args["content"].(string)
	propagate, _ := args["propagate"].(bool)

	if fileName == "" || content == "" {
		return ErrorResult("file_name and content are required for set_context_file")
	}
	if !isAllowedContextFile(fileName) {
		return ErrorResult(fmt.Sprintf("file %q is not a valid context file. Allowed: %s", fileName, strings.Join(allowedContextFiles, ", ")))
	}
	if !settings.isFileTypeAllowed(fileName) {
		return ErrorResult(fmt.Sprintf("file type %q is not allowed by settings. Allowed: %s", fileName, settings.AllowedFileTypes))
	}

	targetAgentID, err := t.resolveAgentID(ctx, agentKey, agentIDStr)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Self-modification guard
	if targetAgentID == callingAgentID {
		return ErrorResult("cannot modify your own agent's context files (self-modification guard)")
	}

	// Protected agent guard
	agent, err := t.agentStore.GetByID(ctx, targetAgentID)
	if err != nil || agent == nil {
		return ErrorResult(fmt.Sprintf("agent not found: %s", targetAgentID))
	}
	if settings.isProtectedAgent(agent.AgentKey) {
		return ErrorResult(fmt.Sprintf("agent %q is protected and cannot be modified", agent.AgentKey))
	}

	// Rate limit
	callerIDStr := callingAgentID.String()
	if !t.counter.canWrite(callerIDStr, settings.MaxFileWritesPerHour) {
		return ErrorResult(fmt.Sprintf("write limit reached (%d per hour). Wait and try again.", settings.MaxFileWritesPerHour))
	}

	// Write the file
	if err := t.agentStore.SetAgentContextFile(ctx, targetAgentID, fileName, content); err != nil {
		slog.Warn("agent_admin.set_context_file failed", "error", err, "agent", targetAgentID, "file", fileName)
		return ErrorResult(fmt.Sprintf("failed to write %s: %v", fileName, err))
	}
	t.counter.incWrite(callerIDStr)

	result := fmt.Sprintf("Written %s for agent %s (key: %s)", fileName, targetAgentID, agent.AgentKey)

	if propagate {
		count, propErr := t.agentStore.PropagateContextFile(ctx, targetAgentID, fileName)
		if propErr != nil {
			slog.Warn("agent_admin.propagate failed", "error", propErr)
			result += fmt.Sprintf("\n⚠️ Propagation failed: %v", propErr)
		} else {
			result += fmt.Sprintf("\nPropagated to %d user instances.", count)
		}
	}

	slog.Info("agent_admin.set_context_file", "agent", agent.AgentKey, "file", fileName, "propagate", propagate, "caller", callingAgentID)
	return NewResult(result)
}

// handleListTeams returns all teams.
func (t *AgentAdminTool) handleListTeams(ctx context.Context) *Result {
	teams, err := t.teamStore.ListTeams(ctx)
	if err != nil {
		slog.Warn("agent_admin.list_teams failed", "error", err)
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

// handleGetTeam returns detailed info for a single team including members.
func (t *AgentAdminTool) handleGetTeam(ctx context.Context, args map[string]any) *Result {
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

// resolveAgentID resolves agent_key or agent_id string to a UUID.
func (t *AgentAdminTool) resolveAgentID(ctx context.Context, agentKey, agentIDStr string) (uuid.UUID, error) {
	if agentIDStr != "" {
		return uuid.Parse(agentIDStr)
	}
	if agentKey != "" {
		agent, err := t.agentStore.GetByKey(ctx, agentKey)
		if err != nil || agent == nil {
			return uuid.Nil, fmt.Errorf("agent not found by key: %s", agentKey)
		}
		return agent.ID, nil
	}
	return uuid.Nil, fmt.Errorf("agent_key or agent_id is required")
}
