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
// Shared across all 4 agent admin tools — reads from "agent_admin" settings key.
type agentAdminSettings struct {
	MaxFileWritesPerHour   int    `json:"max_file_writes_per_hour"`
	AllowedFileTypes       string `json:"allowed_file_types"`
	ProtectedAgentKeys     string `json:"protected_agent_keys"`
	ReadOnly               bool   `json:"read_only"`
	MaxAgentCreatesPerHour int    `json:"max_agent_creates_per_hour"`
	MaxTeamCreatesPerHour  int    `json:"max_team_creates_per_hour"`
	MaxSkillGrantsPerHour  int    `json:"max_skill_grants_per_hour"`
	AllowedAgentTypes      string `json:"allowed_agent_types"`
	DenyAgentDelete        bool   `json:"deny_agent_delete"`
	DenyTeamDelete         bool   `json:"deny_team_delete"`
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

func (s agentAdminSettings) isFileTypeAllowed(fileName string) bool {
	if s.AllowedFileTypes == "" {
		return isAllowedContextFile(fileName)
	}
	for _, t := range strings.Split(s.AllowedFileTypes, ",") {
		t = strings.TrimSpace(t)
		if t == fileName {
			return isAllowedContextFile(fileName)
		}
	}
	return false
}

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

// readAdminSettings reads settings from builtin tool settings context.
// All 4 tools share the "agent_admin" settings key.
func readAdminSettings(ctx context.Context) agentAdminSettings {
	s := defaultAgentAdminSettings()
	if settings := BuiltinToolSettingsFromCtx(ctx); settings != nil {
		if raw, ok := settings["agent_admin"]; ok && len(raw) > 0 {
			_ = json.Unmarshal(raw, &s)
		}
	}
	return s
}

// allowedContextFiles is the list of context files the tools can read/write.
var allowedContextFiles = []string{
	bootstrap.AgentsFile,
	bootstrap.SoulFile,
	bootstrap.IdentityFile,
	bootstrap.UserFile,
	bootstrap.UserPredefinedFile,
	bootstrap.BootstrapFile,
	bootstrap.MemoryJSONFile,
	bootstrap.HeartbeatFile,
}

func isAllowedContextFile(name string) bool {
	for _, f := range allowedContextFiles {
		if f == name {
			return true
		}
	}
	return false
}

// adminCounter tracks write operations per agent within a time window.
// Shared across agent_edit and agent_provision tools.
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

// resolveAgentID resolves agent_key or agent_id string to a UUID.
func resolveAgentID(ctx context.Context, agentStore store.AgentStore, agentKey, agentIDStr string) (uuid.UUID, error) {
	if agentIDStr != "" {
		return uuid.Parse(agentIDStr)
	}
	if agentKey != "" {
		agent, err := agentStore.GetByKey(ctx, agentKey)
		if err != nil || agent == nil {
			return uuid.Nil, fmt.Errorf("agent not found by key: %s", agentKey)
		}
		return agent.ID, nil
	}
	return uuid.Nil, fmt.Errorf("agent_key or agent_id is required")
}

// resolveSkillID resolves skill_id or skill_slug to a UUID.
func resolveSkillID(ctx context.Context, skillStore store.SkillManageStore, skillIDStr, skillSlug string) (uuid.UUID, error) {
	if skillIDStr != "" {
		return uuid.Parse(skillIDStr)
	}
	if skillSlug != "" {
		info, ok := skillStore.GetSkill(ctx, skillSlug)
		if !ok {
			return uuid.Nil, fmt.Errorf("skill not found by slug: %s", skillSlug)
		}
		if info.ID != "" {
			skillUUID, err := uuid.Parse(info.ID)
			if err != nil {
				return uuid.Nil, fmt.Errorf("invalid skill ID: %s", info.ID)
			}
			return skillUUID, nil
		}
		return uuid.Nil, fmt.Errorf("skill %q has no database ID", skillSlug)
	}
	return uuid.Nil, fmt.Errorf("skill_id or skill_slug is required")
}

// teamMemberIDsForLead returns the set of agent IDs visible to a team lead.
// This includes the lead itself plus all members of teams where the agent is lead.
func teamMemberIDsForLead(ctx context.Context, teamStore store.TeamStore, leadAgentID uuid.UUID) (map[uuid.UUID]bool, error) {
	teams, err := teamStore.ListTeams(ctx)
	if err != nil {
		return nil, err
	}
	visible := map[uuid.UUID]bool{leadAgentID: true}
	for _, tm := range teams {
		if tm.LeadAgentID != leadAgentID {
			continue
		}
		members, err := teamStore.ListMembers(ctx, tm.ID)
		if err != nil {
			slog.Warn("teamMemberIDsForLead: failed to list members", "team", tm.ID, "error", err)
			continue
		}
		for _, m := range members {
			visible[m.AgentID] = true
		}
	}
	return visible, nil
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
