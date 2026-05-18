//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteSkillStore_StoreMissingDeps_PersistsForCustomSkills(t *testing.T) {
	ctx, skillStore := newTestSQLiteSkillStore(t)
	skillID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Custom Skill",
		Slug:       "custom-skill",
		OwnerID:    "user-1",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "custom-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	missing := []string{"pip:requests", "npm:tsx"}
	if err := skillStore.StoreMissingDeps(ctx, skillID, missing); err != nil {
		t.Fatalf("StoreMissingDeps error: %v", err)
	}

	info, ok := skillStore.GetSkillByID(ctx, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if !reflect.DeepEqual(info.MissingDeps, missing) {
		t.Fatalf("MissingDeps = %v, want %v", info.MissingDeps, missing)
	}
}

func TestSQLiteSkillStore_CreateSkillManaged_PersistsArchivedDependencyState(t *testing.T) {
	ctx, skillStore := newTestSQLiteSkillStore(t)
	missing := []string{"pip:requests"}

	skillID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:        "Archived Skill",
		Slug:        "archived-skill",
		OwnerID:     "user-1",
		Visibility:  "private",
		Status:      "archived",
		MissingDeps: missing,
		FilePath:    filepath.Join(t.TempDir(), "archived-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	info, ok := skillStore.GetSkillByID(ctx, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if info.Status != "archived" {
		t.Fatalf("Status = %q, want archived", info.Status)
	}
	if !reflect.DeepEqual(info.MissingDeps, missing) {
		t.Fatalf("MissingDeps = %v, want %v", info.MissingDeps, missing)
	}
}

func TestSQLiteSkillStore_GrantToAgentRejectsCrossTenantSkill(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantA, agentA := seedSQLiteTenantAgent(t, db)
	tenantB, _ := seedSQLiteTenantAgent(t, db)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	skillID, err := skillStore.CreateSkillManaged(ctxB, store.SkillCreateParams{
		Name:       "Tenant B Skill",
		Slug:       "tenant-b-skill-" + tenantB.String()[:8],
		OwnerID:    "user-1",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "tenant-b-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	if err := skillStore.GrantToAgent(ctxA, skillID, agentA, 1, "user-1", true); err == nil {
		t.Fatal("GrantToAgent allowed tenant A to grant tenant B skill")
	}

	grants, err := skillStore.ListAgentGrantsForSkill(ctxB, skillID)
	if err != nil {
		t.Fatalf("ListAgentGrantsForSkill error: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("cross-tenant grant was inserted: %+v", grants)
	}

	got, ok := skillStore.GetSkillByID(ctxB, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if got.Visibility != "private" {
		t.Fatalf("cross-tenant grant changed visibility to %q, want private", got.Visibility)
	}
}

func TestSQLiteSkillStore_RevokeFromAgentDoesNotDemoteCrossTenantSkill(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantA, agentA := seedSQLiteTenantAgent(t, db)
	tenantB, _ := seedSQLiteTenantAgent(t, db)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	skillID, err := skillStore.CreateSkillManaged(ctxB, store.SkillCreateParams{
		Name:       "Tenant B Skill",
		Slug:       "tenant-b-revoke-skill-" + tenantB.String()[:8],
		OwnerID:    "user-1",
		Visibility: "internal",
		FilePath:   filepath.Join(t.TempDir(), "tenant-b-revoke-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	if err := skillStore.RevokeFromAgent(ctxA, skillID, agentA); err == nil {
		t.Fatal("RevokeFromAgent allowed tenant A to revoke tenant B skill")
	}

	got, ok := skillStore.GetSkillByID(ctxB, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if got.Visibility != "internal" {
		t.Fatalf("cross-tenant revoke demoted visibility to %q, want internal", got.Visibility)
	}
}

func newTestSQLiteSkillStore(t *testing.T) (context.Context, *SQLiteSkillStore) {
	ctx, skillStore, _ := newTestSQLiteSkillStoreWithDB(t)
	return ctx, skillStore
}

func newTestSQLiteSkillStoreWithDB(t *testing.T) (context.Context, *SQLiteSkillStore, *sql.DB) {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatalf("OpenDB error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema error: %v", err)
	}

	return store.WithTenantID(context.Background(), store.MasterTenantID), NewSQLiteSkillStore(db, t.TempDir()), db
}

func seedSQLiteTenantAgent(t *testing.T, db *sql.DB) (uuid.UUID, uuid.UUID) {
	t.Helper()

	tenantID := uuid.New()
	agentID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES (?, ?, ?, 'active')`,
		tenantID.String(), "tenant-"+tenantID.String()[:8], "t"+tenantID.String()[:8],
	); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?, ?, ?, 'predefined', 'active', 'test', 'test-model', 'user-1')`,
		agentID.String(), tenantID.String(), "agent-"+agentID.String()[:8],
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	return tenantID, agentID
}
