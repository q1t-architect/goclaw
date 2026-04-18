package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeVaultStore embeds store.VaultStore (nil) so the struct satisfies the
// interface at compile time; only GetDocumentByID is implemented for tests.
// Any call to an unimplemented method would nil-pointer panic — which is fine
// because vault_read.Execute only hits GetDocumentByID.
type fakeVaultStore struct {
	store.VaultStore
	byID map[string]*store.VaultDocument // key: tenantID + ":" + docID
}

func (f *fakeVaultStore) GetDocumentByID(ctx context.Context, tenantID, id string) (*store.VaultDocument, error) {
	if f.byID == nil {
		return nil, os.ErrNotExist
	}
	doc, ok := f.byID[tenantID+":"+id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return doc, nil
}

// newVaultReadTestTool builds a VaultReadTool with a temp workspace and a
// fake store pre-seeded with docs.
func newVaultReadTestTool(t *testing.T, docs ...*store.VaultDocument) (*VaultReadTool, string) {
	t.Helper()
	ws := t.TempDir()
	fake := &fakeVaultStore{byID: make(map[string]*store.VaultDocument)}
	for _, d := range docs {
		fake.byID[d.TenantID+":"+d.ID] = d
	}
	tool := NewVaultReadTool()
	tool.SetVaultStore(fake)
	tool.SetWorkspace(ws)
	return tool, ws
}

// writeFile creates a file under workspace with the given relative path.
func writeFile(t *testing.T, ws, rel, content string) {
	t.Helper()
	full := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// writeBytes creates a file with raw bytes (for binary content tests).
func writeBytes(t *testing.T, ws, rel string, b []byte) {
	t.Helper()
	full := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func makeCtx(tenantID, agentID uuid.UUID) context.Context {
	ctx := context.Background()
	if tenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, tenantID)
	}
	if agentID != uuid.Nil {
		ctx = store.WithAgentID(ctx, agentID)
	}
	return ctx
}

func makeCtxWithTeam(tenantID, agentID uuid.UUID, teamID string) context.Context {
	ctx := makeCtx(tenantID, agentID)
	return store.WithRunContext(ctx, &store.RunContext{
		TenantID: tenantID,
		AgentID:  agentID,
		TeamID:   teamID,
	})
}

// --- 1. shared scope → allow, content returned. ---
func TestVaultRead_SharedScope_Allow(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "shared/notes.md", Title: "Notes",
		DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "shared/notes.md", "hello world")

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "hello world") {
		t.Fatalf("content missing: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Notes") {
		t.Fatalf("title missing: %s", res.ForLLM)
	}
}

// --- 2. personal scope, AgentID matches ctx → allow. ---
func TestVaultRead_PersonalScope_Match_Allow(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	aid := agentID.String()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		AgentID: &aid, Scope: "personal", Path: "memo.md",
		Title: "Memo", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "memo.md", "personal body")

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "personal body") {
		t.Fatalf("content missing: %s", res.ForLLM)
	}
}

// --- 3. personal scope, AgentID mismatch → deny. ---
func TestVaultRead_PersonalScope_Mismatch_Deny(t *testing.T) {
	tenantID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()
	docID := uuid.New()
	aid := agentA.String()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		AgentID: &aid, Scope: "personal", Path: "memo.md",
		Title: "Memo", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "memo.md", "personal body")

	res := tool.Execute(makeCtx(tenantID, agentB),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "not accessible") {
		t.Fatalf("expected access denied, got: %s", res.ForLLM)
	}
}

// --- 4. team scope, RunContext.TeamID matches → allow. ---
func TestVaultRead_TeamScope_Match_Allow(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	teamID := uuid.New().String()
	tid := teamID
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		TeamID: &tid, Scope: "team", Path: "team/doc.md",
		Title: "Team Doc", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "team/doc.md", "team body")

	res := tool.Execute(makeCtxWithTeam(tenantID, agentID, teamID),
		map[string]any{"doc_id": docID.String()})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "team body") {
		t.Fatalf("content missing: %s", res.ForLLM)
	}
}

// --- 5. team scope, no run-context / mismatch → deny. ---
func TestVaultRead_TeamScope_NoContext_Deny(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	tid := uuid.New().String()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		TeamID: &tid, Scope: "team", Path: "team/doc.md",
		Title: "Team Doc", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "team/doc.md", "team body")

	// no team in ctx.
	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError {
		t.Fatalf("expected deny, got: %s", res.ForLLM)
	}

	// mismatched team in ctx.
	res2 := tool.Execute(makeCtxWithTeam(tenantID, agentID, uuid.New().String()),
		map[string]any{"doc_id": docID.String()})
	if !res2.IsError {
		t.Fatalf("expected deny on mismatch, got: %s", res2.ForLLM)
	}
}

// --- 6. cross-tenant (different tenant in ctx) → not-found. ---
func TestVaultRead_CrossTenant_NotFound(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantA.String(),
		Scope: "shared", Path: "a.md", Title: "A", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "a.md", "body")

	res := tool.Execute(makeCtx(tenantB, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("expected not found, got: %s", res.ForLLM)
	}
}

// --- 7. missing doc id → not-found. ---
func TestVaultRead_MissingDoc_NotFound(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	tool, _ := newVaultReadTestTool(t)

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": uuid.New().String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("expected not found, got: %s", res.ForLLM)
	}
}

// --- 8. invalid UUID → arg error. ---
func TestVaultRead_InvalidUUID(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	tool, _ := newVaultReadTestTool(t)

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": "not-a-uuid"})
	if !res.IsError || !strings.Contains(res.ForLLM, "invalid doc_id") {
		t.Fatalf("expected invalid doc_id error, got: %s", res.ForLLM)
	}
}

// --- 9. media DocType → rejected with hint. ---
func TestVaultRead_MediaDocType_Rejected(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "pic.png", Title: "Pic", DocType: "media",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "pic.png", "pretend-png")

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "read_image") {
		t.Fatalf("expected media-handler hint, got: %s", res.ForLLM)
	}
}

// --- 10. binary extension even when DocType!=media → rejected by blocklist. ---
func TestVaultRead_BinaryExtension_Rejected(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "report.PDF", Title: "Rep", DocType: "document",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "report.PDF", "%PDF-1.7")

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, ".pdf") {
		t.Fatalf("expected pdf blocklist error, got: %s", res.ForLLM)
	}
}

// --- 11. text extension but binary bytes → rejected by UTF-8 sniff. ---
func TestVaultRead_BinaryContent_UTF8Rejected(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "blob.txt", Title: "Blob", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	// Invalid UTF-8: stray 0xC3 byte with no continuation.
	writeBytes(t, ws, "blob.txt", []byte{0xC3, 0x28, 0xFF, 0xFE, 0xFD})

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "UTF-8") {
		t.Fatalf("expected UTF-8 rejection, got: %s", res.ForLLM)
	}
}

// --- 12. oversize file → truncated with marker. ---
func TestVaultRead_Oversize_Truncated(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "big.md", Title: "Big", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	// 30KB of 'a' — fits in UTF-8 sniff check, but larger than max_bytes below.
	big := strings.Repeat("a", 30_000)
	writeFile(t, ws, "big.md", big)

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String(), "max_bytes": float64(10_000)})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "truncated") {
		t.Fatalf("expected truncation marker, got: %s", res.ForLLM)
	}
}

// --- 13. symlink escaping workspace → denied. ---
func TestVaultRead_SymlinkEscape_Denied(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "escape.md", Title: "Esc", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)

	// Create target outside workspace.
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("leak"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	// Symlink ws/escape.md → outside/secret.txt.
	if err := os.Symlink(target, filepath.Join(ws, "escape.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "outside workspace") {
		t.Fatalf("expected symlink escape denied, got: %s", res.ForLLM)
	}
}

// --- 14. max_bytes clamp to ceiling (1MB). ---
func TestVaultRead_MaxBytes_ClampCeiling(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "c.md", Title: "C", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	// 1.2MB body — should be truncated to 1MB hard ceiling regardless of arg.
	body := bytes.Repeat([]byte("x"), 1_200_000)
	writeBytes(t, ws, "c.md", body)

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{
		"doc_id":    docID.String(),
		"max_bytes": float64(5_000_000), // above ceiling
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "truncated") {
		t.Fatalf("expected truncation marker for ceiling clamp, got head: %s", res.ForLLM[:min(200, len(res.ForLLM))])
	}
}
