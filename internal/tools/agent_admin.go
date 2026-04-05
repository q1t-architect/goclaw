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
	MaxFileWritesPerHour   int    `json:"max_file_writes_per_hour"`    // default: 20
	AllowedFileTypes       string `json:"allowed_file_types"`          // comma-separated, empty = all 8 files
	ProtectedAgentKeys     string `json:"protected_agent_keys"`        // comma-separated agent keys that cannot be modified
	ReadOnly               bool   `json:"read_only"`                   // if true, disables all write operations
	MaxAgentCreatesPerHour int    `json:"max_agent_creates_per_hour"`  // default: 5
	MaxTeamCreatesPerHour  int    `json:"max_team_creates_per_hour"`   // default: 5
	MaxSkillGrantsPerHour  int    `json:"max_skill_grants_per_hour"`   // default: 20
	AllowedAgentTypes      string `json:"allowed_agent_types"`         // comma-separated, empty = open+predefined
	DenyAgentDelete        bool   `json:"deny_agent_delete"`           // if true, delete_agent disabled
	DenyTeamDelete         bool   `json:"deny_team_delete"`            // if true, delete_team disabled
}

func defaultAgentAdminSettings() agentAdminSettings {
	return agentAdminSettings{
		MaxFileWritesPerHour:   20,
		MaxAgentCreatesPerHour: 5,
		MaxTeamCreatesPerHour:  5,
		MaxSkillGrantsPerHour:  20,
	}
}

func (s agentAdminSettings) isAgentTypeAllowed(agentType string) bool {
	if s.AllowedAgentTypes == "" {
		return agentType == "open" || agentType == "predefined"
	}
	for _, t := range strings.Split(s.AllowedAgentTypes, ",") {
		if strings.TrimSpace(t) == agentType {
			return true
		}
	}
	return false
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
	creates  int
	grants   int
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

func (c *adminCounter) canCreate(agentID string, limit int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(agentID).creates < limit
}

func (c *adminCounter) incCreate(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.get(agentID)
	r.creates++
	r.lastSeen = time.Now()
}

func (c *adminCounter) canGrant(agentID string, limit int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(agentID).grants < limit
}

func (c *adminCounter) incGrant(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.get(agentID)
	r.grants++
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
	skillStore store.SkillManageStore // Phase 4
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

func (t *AgentAdminTool) SetSkillManageStore(s store.SkillManageStore) {
	t.skillStore = s
}

func (t *AgentAdminTool) Name() string { return "agent_admin" }

func (t *AgentAdminTool) Description() string {
	return "Manage agents, teams, and skill grants: read/write configurations, CRUD agents/teams, manage members and skill assignments. " +
		"Actions: list_agents, get_agent, get_context_file, set_context_file, " +
		"create_agent, update_agent, delete_agent, " +
		"list_teams, get_team, create_team, update_team, delete_team, " +
		"add_team_member, remove_team_member, " +
		"list_skill_grants, grant_skill, revoke_skill. " +
		"IMPORTANT: Cannot modify your own agent configuration (self-modification guard). " +
		"Disabled by default — must be enabled by admin."
}

func (t *AgentAdminTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list_agents", "get_agent", "get_context_file", "set_context_file", "list_teams", "get_team", "create_agent", "update_agent", "delete_agent", "create_team", "update_team", "delete_team", "add_team_member", "remove_team_member", "list_skill_grants", "grant_skill", "revoke_skill"},
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
			// Agent CRUD params
			"display_name": map[string]any{
				"type":        "string",
				"description": "Agent display name. For create_agent, update_agent.",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "LLM provider. For create_agent, update_agent.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "LLM model. For create_agent, update_agent.",
			},
			"agent_type": map[string]any{
				"type":        "string",
				"description": "Agent type (open/predefined). For create_agent.",
			},
			"context_window": map[string]any{
				"type":        "integer",
				"description": "Context window size. For create_agent, update_agent.",
			},
			"max_tool_iterations": map[string]any{
				"type":        "integer",
				"description": "Max tool iterations. For create_agent, update_agent.",
			},
			"workspace_path": map[string]any{
				"type":        "string",
				"description": "Workspace path. For create_agent, update_agent.",
			},
			"agent_status": map[string]any{
				"type":        "string",
				"description": "Agent status. For update_agent.",
			},
			"target_agent_id": map[string]any{
				"type":        "string",
				"description": "Target agent UUID. Alternative to agent_key for update/delete_agent.",
			},
			// Team params
			"team_id": map[string]any{
				"type":        "string",
				"description": "Team UUID. Required for get_team, update_team, delete_team, add/remove_team_member.",
			},
			"team_name": map[string]any{
				"type":        "string",
				"description": "Team name. For create_team, update_team.",
			},
			"team_description": map[string]any{
				"type":        "string",
				"description": "Team description. For create_team, update_team.",
			},
			"team_status": map[string]any{
				"type":        "string",
				"description": "Team status. For update_team.",
			},
			"lead_agent_id": map[string]any{
				"type":        "string",
				"description": "Lead agent UUID. Required for create_team.",
			},
			// Member params
			"member_agent_id": map[string]any{
				"type":        "string",
				"description": "Member agent UUID. For add/remove_team_member.",
			},
			"member_role": map[string]any{
				"type":        "string",
				"description": "Member role (lead/member/reviewer). For add_team_member.",
			},
			// Skill params
			"skill_id": map[string]any{
				"type":        "string",
				"description": "Skill UUID. For grant_skill, revoke_skill.",
			},
			"skill_slug": map[string]any{
				"type":        "string",
				"description": "Skill slug. Alternative to skill_id for grant/revoke_skill.",
			},
			"skill_version": map[string]any{
				"type":        "integer",
				"description": "Skill version. For grant_skill. Default: latest.",
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
	// Phase 3: Agent CRUD
	case "create_agent":
		return t.handleCreateAgent(ctx, args, callingAgentID)
	case "update_agent":
		return t.handleUpdateAgent(ctx, args, callingAgentID)
	case "delete_agent":
		return t.handleDeleteAgent(ctx, args, callingAgentID)
	// Phase 4: Team CRUD + Members + Skills
	case "create_team":
		return t.handleCreateTeam(ctx, args, callingAgentID)
	case "update_team":
		return t.handleUpdateTeam(ctx, args, callingAgentID)
	case "delete_team":
		return t.handleDeleteTeam(ctx, args, callingAgentID)
	case "add_team_member":
		return t.handleAddTeamMember(ctx, args, callingAgentID)
	case "remove_team_member":
		return t.handleRemoveTeamMember(ctx, args, callingAgentID)
	case "list_skill_grants":
		return t.handleListSkillGrants(ctx, args, callingAgentID)
	case "grant_skill":
		return t.handleGrantSkill(ctx, args, callingAgentID)
	case "revoke_skill":
		return t.handleRevokeSkill(ctx, args, callingAgentID)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %q", action))
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


// --- Phase 3: Agent CRUD ---

func (t *AgentAdminTool) handleCreateAgent(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
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
		slog.Warn("agent_admin.create_agent failed", "error", err, "key", agentKey)
		return ErrorResult(fmt.Sprintf("failed to create agent: %v", err))
	}
	t.counter.incCreate(callerIDStr)
	slog.Info("agent_admin.create_agent", "key", agentKey, "id", agent.ID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Agent created: %s (key: %s, id: %s, type: %s)", displayName, agentKey, agent.ID, agentType))
}

func (t *AgentAdminTool) handleUpdateAgent(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	targetAgentID, err := t.resolveAgentID(ctx, strArg(args, "agent_key"), strArg(args, "target_agent_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	if targetAgentID == callingAgentID {
		return ErrorResult("cannot update your own agent (self-modification guard)")
	}
	agent, err := t.agentStore.GetByID(ctx, targetAgentID)
	if err != nil || agent == nil {
		return ErrorResult(fmt.Sprintf("agent not found: %s", targetAgentID))
	}
	if settings.isProtectedAgent(agent.AgentKey) {
		return ErrorResult(fmt.Sprintf("agent %q is protected", agent.AgentKey))
	}
	callerIDStr := callingAgentID.String()
	if !t.counter.canWrite(callerIDStr, settings.MaxFileWritesPerHour) {
		return ErrorResult(fmt.Sprintf("write limit reached (%d/hour).", settings.MaxFileWritesPerHour))
	}
	updates := map[string]any{}
	if v, ok := args["display_name"].(string); ok && v != "" {
		updates["display_name"] = v
	}
	if v, ok := args["provider"].(string); ok && v != "" {
		updates["provider"] = v
	}
	if v, ok := args["model"].(string); ok && v != "" {
		updates["model"] = v
	}
	if v, ok := args["context_window"].(float64); ok {
		updates["context_window"] = int(v)
	}
	if v, ok := args["max_tool_iterations"].(float64); ok {
		updates["max_tool_iterations"] = int(v)
	}
	if v, ok := args["workspace_path"].(string); ok && v != "" {
		updates["workspace"] = v
	}
	if v, ok := args["agent_status"].(string); ok && v != "" {
		updates["status"] = v
	}
	if len(updates) == 0 {
		return ErrorResult("no updatable fields provided. Allowed: display_name, provider, model, context_window, max_tool_iterations, workspace_path, agent_status")
	}
	if err := t.agentStore.Update(ctx, targetAgentID, updates); err != nil {
		slog.Warn("agent_admin.update_agent failed", "error", err, "id", targetAgentID)
		return ErrorResult(fmt.Sprintf("failed to update agent: %v", err))
	}
	t.counter.incWrite(callerIDStr)
	slog.Info("agent_admin.update_agent", "id", targetAgentID, "fields", len(updates), "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Agent %s updated: %d fields changed (%v)", agent.AgentKey, len(updates), mapKeys(updates)))
}

func (t *AgentAdminTool) handleDeleteAgent(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	if settings.DenyAgentDelete {
		return ErrorResult("agent deletion is disabled by policy (deny_agent_delete).")
	}
	targetAgentID, err := t.resolveAgentID(ctx, strArg(args, "agent_key"), strArg(args, "target_agent_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	if targetAgentID == callingAgentID {
		return ErrorResult("cannot delete your own agent (self-deletion guard)")
	}
	agent, err := t.agentStore.GetByID(ctx, targetAgentID)
	if err != nil || agent == nil {
		return ErrorResult(fmt.Sprintf("agent not found: %s", targetAgentID))
	}
	if settings.isProtectedAgent(agent.AgentKey) {
		return ErrorResult(fmt.Sprintf("agent %q is protected", agent.AgentKey))
	}
	team, _ := t.teamStore.GetTeamForAgent(ctx, targetAgentID)
	if team != nil && team.Status == "active" {
		return ErrorResult(fmt.Sprintf("agent %q is lead of active team %q (id: %s). Remove from team first.", agent.AgentKey, team.Name, team.ID))
	}
	if err := t.agentStore.Delete(ctx, targetAgentID); err != nil {
		slog.Warn("agent_admin.delete_agent failed", "error", err, "id", targetAgentID)
		return ErrorResult(fmt.Sprintf("failed to delete agent: %v", err))
	}
	slog.Info("agent_admin.delete_agent", "key", agent.AgentKey, "id", targetAgentID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Agent deleted: %s (key: %s)", agent.DisplayName, agent.AgentKey))
}

// --- Phase 4: Team CRUD + Members + Skills ---

func (t *AgentAdminTool) handleCreateTeam(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	name, _ := args["team_name"].(string)
	description, _ := args["team_description"].(string)
	leadAgentIDStr, _ := args["lead_agent_id"].(string)
	if name == "" || leadAgentIDStr == "" {
		return ErrorResult("team_name and lead_agent_id are required for create_team")
	}
	leadID, err := uuid.Parse(leadAgentIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid lead_agent_id: %v", err))
	}
	leadAgent, err := t.agentStore.GetByID(ctx, leadID)
	if err != nil || leadAgent == nil {
		return ErrorResult(fmt.Sprintf("lead agent not found: %s", leadAgentIDStr))
	}
	callerIDStr := callingAgentID.String()
	if !t.counter.canCreate(callerIDStr, settings.MaxTeamCreatesPerHour) {
		return ErrorResult(fmt.Sprintf("team create limit reached (%d/hour).", settings.MaxTeamCreatesPerHour))
	}
	team := &store.TeamData{
		Name:        name,
		LeadAgentID: leadID,
		Description: description,
		Status:      "active",
	}
	if err := t.teamStore.CreateTeam(ctx, team); err != nil {
		slog.Warn("agent_admin.create_team failed", "error", err, "name", name)
		return ErrorResult(fmt.Sprintf("failed to create team: %v", err))
	}
	t.counter.incCreate(callerIDStr)
	slog.Info("agent_admin.create_team", "name", name, "id", team.ID, "lead", leadAgent.AgentKey, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Team created: %s (id: %s, lead: %s [%s])", name, team.ID, leadAgent.DisplayName, leadAgent.AgentKey))
}

func (t *AgentAdminTool) handleUpdateTeam(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	teamIDStr, _ := args["team_id"].(string)
	if teamIDStr == "" {
		return ErrorResult("team_id is required for update_team")
	}
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid team_id: %v", err))
	}
	updates := map[string]any{}
	if v, ok := args["team_name"].(string); ok && v != "" {
		updates["name"] = v
	}
	if v, ok := args["team_description"].(string); ok {
		updates["description"] = v
	}
	if v, ok := args["team_status"].(string); ok && v != "" {
		updates["status"] = v
	}
	if len(updates) == 0 {
		return ErrorResult("no updatable fields. Allowed: team_name, team_description, team_status")
	}
	if err := t.teamStore.UpdateTeam(ctx, teamID, updates); err != nil {
		slog.Warn("agent_admin.update_team failed", "error", err, "id", teamID)
		return ErrorResult(fmt.Sprintf("failed to update team: %v", err))
	}
	slog.Info("agent_admin.update_team", "id", teamID, "fields", len(updates), "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Team %s updated: %d fields changed", teamID, len(updates)))
}

func (t *AgentAdminTool) handleDeleteTeam(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	if settings.DenyTeamDelete {
		return ErrorResult("team deletion is disabled by policy (deny_team_delete).")
	}
	teamIDStr, _ := args["team_id"].(string)
	if teamIDStr == "" {
		return ErrorResult("team_id is required for delete_team")
	}
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid team_id: %v", err))
	}
	team, err := t.teamStore.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		return ErrorResult(fmt.Sprintf("team not found: %s", teamIDStr))
	}
	if err := t.teamStore.DeleteTeam(ctx, teamID); err != nil {
		slog.Warn("agent_admin.delete_team failed", "error", err, "id", teamID)
		return ErrorResult(fmt.Sprintf("failed to delete team: %v", err))
	}
	slog.Info("agent_admin.delete_team", "name", team.Name, "id", teamID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Team deleted: %s (id: %s)", team.Name, teamID))
}

func (t *AgentAdminTool) handleAddTeamMember(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	teamIDStr, _ := args["team_id"].(string)
	memberIDStr, _ := args["member_agent_id"].(string)
	role, _ := args["member_role"].(string)
	if teamIDStr == "" || memberIDStr == "" || role == "" {
		return ErrorResult("team_id, member_agent_id, and member_role are required")
	}
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid team_id: %v", err))
	}
	memberID, err := uuid.Parse(memberIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid member_agent_id: %v", err))
	}
	validRoles := map[string]bool{"lead": true, "member": true, "reviewer": true}
	if !validRoles[role] {
		return ErrorResult(fmt.Sprintf("invalid role %q. Allowed: lead, member, reviewer", role))
	}
	member, err := t.agentStore.GetByID(ctx, memberID)
	if err != nil || member == nil {
		return ErrorResult(fmt.Sprintf("agent not found: %s", memberIDStr))
	}
	if err := t.teamStore.AddMember(ctx, teamID, memberID, role); err != nil {
		slog.Warn("agent_admin.add_team_member failed", "error", err, "team", teamID, "agent", memberID)
		return ErrorResult(fmt.Sprintf("failed to add member: %v", err))
	}
	slog.Info("agent_admin.add_team_member", "team", teamID, "agent", member.AgentKey, "role", role, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Added %s (%s) to team %s as %s", member.DisplayName, member.AgentKey, teamID, role))
}

func (t *AgentAdminTool) handleRemoveTeamMember(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	teamIDStr, _ := args["team_id"].(string)
	memberIDStr, _ := args["member_agent_id"].(string)
	if teamIDStr == "" || memberIDStr == "" {
		return ErrorResult("team_id and member_agent_id are required")
	}
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid team_id: %v", err))
	}
	memberID, err := uuid.Parse(memberIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid member_agent_id: %v", err))
	}
	team, err := t.teamStore.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		return ErrorResult(fmt.Sprintf("team not found: %s", teamIDStr))
	}
	if team.LeadAgentID == memberID {
		return ErrorResult("cannot remove team lead. Transfer leadership or delete the team instead.")
	}
	if err := t.teamStore.RemoveMember(ctx, teamID, memberID); err != nil {
		slog.Warn("agent_admin.remove_team_member failed", "error", err, "team", teamID, "agent", memberID)
		return ErrorResult(fmt.Sprintf("failed to remove member: %v", err))
	}
	slog.Info("agent_admin.remove_team_member", "team", teamID, "agent", memberID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Removed agent %s from team %s", memberID, teamID))
}

func (t *AgentAdminTool) handleListSkillGrants(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	if t.skillStore == nil {
		return ErrorResult("skill management is not available (skill store not wired)")
	}
	targetAgentID, err := t.resolveAgentID(ctx, strArg(args, "agent_key"), strArg(args, "agent_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	skills, err := t.skillStore.ListWithGrantStatus(ctx, targetAgentID)
	if err != nil {
		slog.Warn("agent_admin.list_skill_grants failed", "error", err, "agent", targetAgentID)
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

func (t *AgentAdminTool) handleGrantSkill(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	if t.skillStore == nil {
		return ErrorResult("skill management is not available")
	}
	targetAgentID, err := t.resolveAgentID(ctx, strArg(args, "agent_key"), strArg(args, "agent_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	skillIDStr, _ := args["skill_id"].(string)
	skillSlug, _ := args["skill_slug"].(string)
	version, _ := args["skill_version"].(float64)
	if skillIDStr == "" && skillSlug == "" {
		return ErrorResult("skill_id or skill_slug is required for grant_skill")
	}
	var skillUUID uuid.UUID
	if skillIDStr != "" {
		skillUUID, err = uuid.Parse(skillIDStr)
		if err != nil {
			return ErrorResult(fmt.Sprintf("invalid skill_id: %v", err))
		}
	} else {
		info, ok := t.skillStore.GetSkill(ctx, skillSlug)
		if !ok {
			return ErrorResult(fmt.Sprintf("skill not found by slug: %s", skillSlug))
		}
		if info.ID != "" {
			skillUUID, _ = uuid.Parse(info.ID)
		}
		if skillUUID == uuid.Nil {
			return ErrorResult(fmt.Sprintf("skill %q has no database ID (may be filesystem-only)", skillSlug))
		}
	}
	callerIDStr := callingAgentID.String()
	if !t.counter.canGrant(callerIDStr, settings.MaxSkillGrantsPerHour) {
		return ErrorResult(fmt.Sprintf("skill grant limit reached (%d/hour).", settings.MaxSkillGrantsPerHour))
	}
	ver := int(version)
	if ver == 0 {
		ver = t.skillStore.GetNextVersion(ctx, skillSlug)
	}
	if err := t.skillStore.GrantToAgent(ctx, skillUUID, targetAgentID, ver, callingAgentID.String()); err != nil {
		slog.Warn("agent_admin.grant_skill failed", "error", err, "skill", skillUUID, "agent", targetAgentID)
		return ErrorResult(fmt.Sprintf("failed to grant skill: %v", err))
	}
	t.counter.incGrant(callerIDStr)
	slog.Info("agent_admin.grant_skill", "skill", skillUUID, "agent", targetAgentID, "version", ver, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Skill %s granted to agent %s (version %d)", skillUUID, targetAgentID, ver))
}

func (t *AgentAdminTool) handleRevokeSkill(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := t.readSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	if t.skillStore == nil {
		return ErrorResult("skill management is not available")
	}
	targetAgentID, err := t.resolveAgentID(ctx, strArg(args, "agent_key"), strArg(args, "agent_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	skillIDStr, _ := args["skill_id"].(string)
	skillSlug, _ := args["skill_slug"].(string)
	if skillIDStr == "" && skillSlug == "" {
		return ErrorResult("skill_id or skill_slug is required for revoke_skill")
	}
	var skillUUID uuid.UUID
	if skillIDStr != "" {
		skillUUID, err = uuid.Parse(skillIDStr)
		if err != nil {
			return ErrorResult(fmt.Sprintf("invalid skill_id: %v", err))
		}
	} else {
		info, ok := t.skillStore.GetSkill(ctx, skillSlug)
		if !ok {
			return ErrorResult(fmt.Sprintf("skill not found by slug: %s", skillSlug))
		}
		if info.ID != "" {
			skillUUID, _ = uuid.Parse(info.ID)
		}
		if skillUUID == uuid.Nil {
			return ErrorResult(fmt.Sprintf("skill %q has no database ID", skillSlug))
		}
	}
	if err := t.skillStore.RevokeFromAgent(ctx, skillUUID, targetAgentID); err != nil {
		slog.Warn("agent_admin.revoke_skill failed", "error", err, "skill", skillUUID, "agent", targetAgentID)
		return ErrorResult(fmt.Sprintf("failed to revoke skill: %v", err))
	}
	slog.Info("agent_admin.revoke_skill", "skill", skillUUID, "agent", targetAgentID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Skill %s revoked from agent %s", skillUUID, targetAgentID))
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

// strArg extracts a string argument from the args map.
func strArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

// mapKeys returns the keys of a map.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
