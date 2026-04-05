package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AgentEditTool provides write access to agent/team configs, context files, and skill grants.
// All modifications are rate-limited and respect self-modification/protected agent guards.
// Disabled by default — admin must enable via builtin_tools settings.
type AgentEditTool struct {
	agentStore store.AgentStore
	teamStore  store.TeamStore
	skillStore store.SkillManageStore
	counter    *adminCounter
}

func NewAgentEditTool() *AgentEditTool {
	return &AgentEditTool{
		counter: newAdminCounter(),
	}
}

func (t *AgentEditTool) SetAgentStore(s store.AgentStore)      { t.agentStore = s }
func (t *AgentEditTool) SetTeamStore(s store.TeamStore)        { t.teamStore = s }
func (t *AgentEditTool) SetSkillManageStore(s store.SkillManageStore) { t.skillStore = s }

func (t *AgentEditTool) Name() string { return "agent_edit" }

func (t *AgentEditTool) Description() string {
	return "Modify agent/team configurations, context files, and skill grants. " +
		"Actions: set_context_file, update_agent, update_team, add_team_member, remove_team_member, grant_skill, revoke_skill. " +
		"IMPORTANT: Cannot modify your own agent configuration (self-modification guard). " +
		"Disabled by default — must be enabled by admin."
}

func (t *AgentEditTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"set_context_file", "update_agent", "update_team", "add_team_member", "remove_team_member", "grant_skill", "revoke_skill"},
				"description": "The admin action to perform",
			},
			// Agent params
			"agent_key": map[string]any{
				"type":        "string",
				"description": "Agent key (e.g. 'default', 'daemon').",
			},
			"agent_id": map[string]any{
				"type":        "string",
				"description": "Agent UUID.",
			},
			"target_agent_id": map[string]any{
				"type":        "string",
				"description": "Target agent UUID. Alternative to agent_key for update_agent.",
			},
			// Context file params
			"file_name": map[string]any{
				"type":        "string",
				"description": "Context file name (e.g. 'SOUL.md', 'IDENTITY.md'). Required for set_context_file.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "File content to write. Required for set_context_file.",
			},
			"propagate": map[string]any{
				"type":        "boolean",
				"description": "If true, propagate the file to all user instances (set_context_file only). Default: false.",
			},
			// Agent update params
			"display_name": map[string]any{
				"type":        "string",
				"description": "Agent display name. For update_agent.",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "LLM provider. For update_agent.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "LLM model. For update_agent.",
			},
			"context_window": map[string]any{
				"type":        "integer",
				"description": "Context window size. For update_agent.",
			},
			"max_tool_iterations": map[string]any{
				"type":        "integer",
				"description": "Max tool iterations. For update_agent.",
			},
			"workspace_path": map[string]any{
				"type":        "string",
				"description": "Workspace path. For update_agent.",
			},
			"agent_status": map[string]any{
				"type":        "string",
				"description": "Agent status. For update_agent.",
			},
			// Team params
			"team_id": map[string]any{
				"type":        "string",
				"description": "Team UUID. Required for update_team, add/remove_team_member.",
			},
			"team_name": map[string]any{
				"type":        "string",
				"description": "Team name. For update_team.",
			},
			"team_description": map[string]any{
				"type":        "string",
				"description": "Team description. For update_team.",
			},
			"team_status": map[string]any{
				"type":        "string",
				"description": "Team status. For update_team.",
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

func (t *AgentEditTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.agentStore == nil || t.teamStore == nil {
		return NewResult("Agent edit tool is not enabled for this agent.")
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
	case "set_context_file":
		return t.handleSetContextFile(ctx, args, callingAgentID)
	case "update_agent":
		return t.handleUpdateAgent(ctx, args, callingAgentID)
	case "update_team":
		return t.handleUpdateTeam(ctx, args, callingAgentID)
	case "add_team_member":
		return t.handleAddTeamMember(ctx, args, callingAgentID)
	case "remove_team_member":
		return t.handleRemoveTeamMember(ctx, args, callingAgentID)
	case "grant_skill":
		return t.handleGrantSkill(ctx, args, callingAgentID)
	case "revoke_skill":
		return t.handleRevokeSkill(ctx, args, callingAgentID)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %q", action))
	}
}

func (t *AgentEditTool) handleSetContextFile(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
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

	targetAgentID, err := resolveAgentID(ctx, t.agentStore, agentKey, agentIDStr)
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
		slog.Warn("agent_edit.set_context_file failed", "error", err, "agent", targetAgentID, "file", fileName)
		return ErrorResult(fmt.Sprintf("failed to write %s: %v", fileName, err))
	}
	t.counter.incWrite(callerIDStr)

	result := fmt.Sprintf("Written %s for agent %s (key: %s)", fileName, targetAgentID, agent.AgentKey)

	if propagate {
		count, propErr := t.agentStore.PropagateContextFile(ctx, targetAgentID, fileName)
		if propErr != nil {
			slog.Warn("agent_edit.propagate failed", "error", propErr)
			result += fmt.Sprintf("\n⚠️ Propagation failed: %v", propErr)
		} else {
			result += fmt.Sprintf("\nPropagated to %d user instances.", count)
		}
	}

	slog.Info("agent_edit.set_context_file", "agent", agent.AgentKey, "file", fileName, "propagate", propagate, "caller", callingAgentID)
	return NewResult(result)
}

func (t *AgentEditTool) handleUpdateAgent(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}

	targetAgentID, err := resolveAgentID(ctx, t.agentStore, strArg(args, "agent_key"), strArg(args, "target_agent_id"))
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
		slog.Warn("agent_edit.update_agent failed", "error", err, "id", targetAgentID)
		return ErrorResult(fmt.Sprintf("failed to update agent: %v", err))
	}
	t.counter.incWrite(callerIDStr)
	slog.Info("agent_edit.update_agent", "id", targetAgentID, "fields", len(updates), "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Agent %s updated: %d fields changed (%v)", agent.AgentKey, len(updates), mapKeys(updates)))
}

func (t *AgentEditTool) handleUpdateTeam(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
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
	team, err := t.teamStore.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		return ErrorResult(fmt.Sprintf("team not found: %s", teamIDStr))
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
		return ErrorResult("no updatable fields provided. Allowed: team_name, team_description, team_status")
	}
	callerIDStr := callingAgentID.String()
	if !t.counter.canWrite(callerIDStr, settings.MaxFileWritesPerHour) {
		return ErrorResult(fmt.Sprintf("write limit reached (%d/hour).", settings.MaxFileWritesPerHour))
	}
	if err := t.teamStore.UpdateTeam(ctx, teamID, updates); err != nil {
		slog.Warn("agent_edit.update_team failed", "error", err, "id", teamID)
		return ErrorResult(fmt.Sprintf("failed to update team: %v", err))
	}
	t.counter.incWrite(callerIDStr)
	slog.Info("agent_edit.update_team", "id", teamID, "fields", len(updates), "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Team %s updated: %d fields changed (%v)", team.Name, len(updates), mapKeys(updates)))
}

func (t *AgentEditTool) handleAddTeamMember(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	teamIDStr, _ := args["team_id"].(string)
	if teamIDStr == "" {
		return ErrorResult("team_id is required for add_team_member")
	}
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid team_id: %v", err))
	}
	memberAgentIDStr, _ := args["member_agent_id"].(string)
	if memberAgentIDStr == "" {
		return ErrorResult("member_agent_id is required for add_team_member")
	}
	memberAgentID, err := uuid.Parse(memberAgentIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid member_agent_id: %v", err))
	}
	role, _ := args["member_role"].(string)
	if role == "" {
		role = "member"
	}
	callerIDStr := callingAgentID.String()
	if !t.counter.canWrite(callerIDStr, settings.MaxFileWritesPerHour) {
		return ErrorResult(fmt.Sprintf("write limit reached (%d/hour).", settings.MaxFileWritesPerHour))
	}
	if err := t.teamStore.AddMember(ctx, teamID, memberAgentID, role); err != nil {
		slog.Warn("agent_edit.add_team_member failed", "error", err, "team", teamID, "agent", memberAgentID)
		return ErrorResult(fmt.Sprintf("failed to add team member: %v", err))
	}
	t.counter.incWrite(callerIDStr)
	slog.Info("agent_edit.add_team_member", "team", teamID, "agent", memberAgentID, "role", role, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Member %s added to team %s with role %s", memberAgentID, teamID, role))
}

func (t *AgentEditTool) handleRemoveTeamMember(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	teamIDStr, _ := args["team_id"].(string)
	if teamIDStr == "" {
		return ErrorResult("team_id is required for remove_team_member")
	}
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid team_id: %v", err))
	}
	memberAgentIDStr, _ := args["member_agent_id"].(string)
	if memberAgentIDStr == "" {
		return ErrorResult("member_agent_id is required for remove_team_member")
	}
	memberAgentID, err := uuid.Parse(memberAgentIDStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid member_agent_id: %v", err))
	}
	callerIDStr := callingAgentID.String()
	if !t.counter.canWrite(callerIDStr, settings.MaxFileWritesPerHour) {
		return ErrorResult(fmt.Sprintf("write limit reached (%d/hour).", settings.MaxFileWritesPerHour))
	}
	if err := t.teamStore.RemoveMember(ctx, teamID, memberAgentID); err != nil {
		slog.Warn("agent_edit.remove_team_member failed", "error", err, "team", teamID, "agent", memberAgentID)
		return ErrorResult(fmt.Sprintf("failed to remove team member: %v", err))
	}
	t.counter.incWrite(callerIDStr)
	slog.Info("agent_edit.remove_team_member", "team", teamID, "agent", memberAgentID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Member %s removed from team %s", memberAgentID, teamID))
}

func (t *AgentEditTool) handleGrantSkill(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	if t.skillStore == nil {
		return ErrorResult("skill management is not available")
	}
	targetAgentID, err := resolveAgentID(ctx, t.agentStore, strArg(args, "agent_key"), strArg(args, "agent_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	skillIDStr, _ := args["skill_id"].(string)
	skillSlug, _ := args["skill_slug"].(string)
	if skillIDStr == "" && skillSlug == "" {
		return ErrorResult("skill_id or skill_slug is required for grant_skill")
	}
	skillUUID, err := resolveSkillID(ctx, t.skillStore, skillIDStr, skillSlug)
	if err != nil {
		return ErrorResult(err.Error())
	}
	callerIDStr := callingAgentID.String()
	if !t.counter.canGrant(callerIDStr, settings.MaxSkillGrantsPerHour) {
		return ErrorResult(fmt.Sprintf("skill grant limit reached (%d/hour).", settings.MaxSkillGrantsPerHour))
	}
	version, _ := args["skill_version"].(float64)
	ver := int(version)
	if ver == 0 {
		ver = t.skillStore.GetNextVersion(ctx, skillSlug)
	}
	if err := t.skillStore.GrantToAgent(ctx, skillUUID, targetAgentID, ver, callingAgentID.String()); err != nil {
		slog.Warn("agent_edit.grant_skill failed", "error", err, "skill", skillUUID, "agent", targetAgentID)
		return ErrorResult(fmt.Sprintf("failed to grant skill: %v", err))
	}
	t.counter.incGrant(callerIDStr)
	slog.Info("agent_edit.grant_skill", "skill", skillUUID, "agent", targetAgentID, "version", ver, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Skill %s granted to agent %s (version %d)", skillUUID, targetAgentID, ver))
}

func (t *AgentEditTool) handleRevokeSkill(ctx context.Context, args map[string]any, callingAgentID uuid.UUID) *Result {
	settings := readAdminSettings(ctx)
	if settings.ReadOnly {
		return ErrorResult("agent_admin is in read-only mode.")
	}
	if t.skillStore == nil {
		return ErrorResult("skill management is not available")
	}
	targetAgentID, err := resolveAgentID(ctx, t.agentStore, strArg(args, "agent_key"), strArg(args, "agent_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	skillIDStr, _ := args["skill_id"].(string)
	skillSlug, _ := args["skill_slug"].(string)
	if skillIDStr == "" && skillSlug == "" {
		return ErrorResult("skill_id or skill_slug is required for revoke_skill")
	}
	skillUUID, err := resolveSkillID(ctx, t.skillStore, skillIDStr, skillSlug)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.skillStore.RevokeFromAgent(ctx, skillUUID, targetAgentID); err != nil {
		slog.Warn("agent_edit.revoke_skill failed", "error", err, "skill", skillUUID, "agent", targetAgentID)
		return ErrorResult(fmt.Sprintf("failed to revoke skill: %v", err))
	}
	slog.Info("agent_edit.revoke_skill", "skill", skillUUID, "agent", targetAgentID, "caller", callingAgentID)
	return NewResult(fmt.Sprintf("Skill %s revoked from agent %s", skillUUID, targetAgentID))
}
