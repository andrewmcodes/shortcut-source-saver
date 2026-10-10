package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/config"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
	"github.com/andrewmcodes/shortcut-source-saver/internal/tui"
)

const (
	originalID   = "D93F4368-D3D6-4695-A5CB-2EFE615A348D"
	conflictID   = "55992F98-1B4D-44CF-94C8-9314FF434579"
	hiddenID     = "11111111-0000-0000-0000-000000000001"
	unselectedID = "22222222-0000-0000-0000-000000000002"
	tombstonedID = "33333333-0000-0000-0000-000000000003"
	goneID       = "44444444-0000-0000-0000-000000000004"
	newcomerID   = "55555555-0000-0000-0000-000000000005"
)

type fixtureRow struct {
	pk         int
	name       string
	id         string
	hidden     bool
	tombstoned bool
	conflictOf int
}

var standardLibrary = []fixtureRow{
	{pk: 1, name: "get_apple_app", id: originalID},
	{pk: 2, name: "_Get App from Input", id: conflictID, conflictOf: 1},
	{pk: 3, name: "Hidden Helper", id: hiddenID, hidden: true},
	{pk: 4, name: "Unselected", id: unselectedID},
	{pk: 5, name: "Tombstoned", id: tombstonedID, tombstoned: true},
}

func openFixtureLibrary(t *testing.T, rows []fixtureRow) *shortcuts.Library {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Shortcuts.sqlite")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE ZSHORTCUT (Z_PK INTEGER PRIMARY KEY, ZNAME TEXT, ZWORKFLOWID TEXT, ZACTIONS INTEGER,
			ZICON INTEGER, ZTOMBSTONED INTEGER, ZACTIONCOUNT INTEGER, ZCREATIONDATE REAL, ZMODIFICATIONDATE REAL,
			ZHIDDENFROMLIBRARYANDSYNC INTEGER, ZCONFLICTOF INTEGER, ZINPUTCLASSESDATA BLOB, ZOUTPUTCLASSESDATA BLOB,
			ZIMPORTQUESTIONSDATA BLOB, ZLASTMIGRATEDCLIENTVERSION TEXT, ZMINIMUMCLIENTVERSION TEXT,
			ZHASOUTPUTFALLBACK INTEGER, ZHASSHORTCUTINPUTVARIABLES INTEGER, ZSHOWINSEARCH INTEGER,
			ZHIDDENFROMWIDGET INTEGER)`,
		`CREATE TABLE ZSHORTCUTACTIONS (Z_PK INTEGER PRIMARY KEY, ZDATA BLOB)`,
		`CREATE TABLE ZSHORTCUTICON (Z_PK INTEGER PRIMARY KEY, ZGLYPHNUMBER INTEGER, ZBACKGROUNDCOLORVALUE INTEGER)`,
		`INSERT INTO ZSHORTCUTACTIONS (Z_PK, ZDATA) VALUES (1, NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range rows {
		var conflictOf any
		if r.conflictOf != 0 {
			conflictOf = r.conflictOf
		}
		if _, err := db.Exec(`INSERT INTO ZSHORTCUT (Z_PK, ZNAME, ZWORKFLOWID, ZACTIONS, ZCREATIONDATE,
			ZHIDDENFROMLIBRARYANDSYNC, ZTOMBSTONED, ZCONFLICTOF) VALUES (?, ?, ?, 1, 0, ?, ?, ?)`,
			r.pk, r.name, r.id, r.hidden, r.tombstoned, conflictOf); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	lib, err := shortcuts.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

func newTestService(t *testing.T, repo string, lib *shortcuts.Library) *Service {
	t.Helper()
	cfg := config.Default()
	cfg.RepoPath = repo
	cfg.IncludeSigned = false
	return New(cfg, lib, Options{})
}

func seedBackup(t *testing.T, repo, slug string, meta archive.Metadata) {
	t.Helper()
	dir := filepath.Join(repo, ShortcutsDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SourceFileName), []byte(`{"WFWorkflowActions": []}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := archive.WriteMetadata(filepath.Join(dir, MetadataFileName), meta); err != nil {
		t.Fatal(err)
	}
}

func readMeta(t *testing.T, repo, slug string) archive.Metadata {
	t.Helper()
	meta, ok, err := readMetadata(filepath.Join(repo, ShortcutsDir, slug))
	if err != nil || !ok {
		t.Fatalf("reading %s metadata: ok=%v err=%v", slug, ok, err)
	}
	return meta
}

func backUp(t *testing.T, svc *Service, workflowIDs ...string) {
	t.Helper()
	all, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, id := range workflowIDs {
		want[id] = true
	}
	for _, sc := range all {
		if !want[sc.WorkflowID] {
			continue
		}
		if r := svc.Backup(context.Background(), sc); r.Outcome == tui.OutcomeFailed {
			t.Fatalf("Backup(%q): %v %s", sc.Name, r.Err, r.Detail)
		}
	}
}

func reconcile(t *testing.T, svc *Service) map[string]StatusChange {
	t.Helper()
	changes, err := svc.ReconcileStatus()
	if err != nil {
		t.Fatalf("ReconcileStatus: %v", err)
	}
	bySlug := map[string]StatusChange{}
	for _, c := range changes {
		bySlug[c.Slug] = c
	}
	return bySlug
}

func seedStandardRepo(t *testing.T) string {
	repo := t.TempDir()
	seedBackup(t, repo, "gone", archive.Metadata{Name: "Gone", WorkflowID: goneID})
	seedBackup(t, repo, "tombstoned", archive.Metadata{Name: "Tombstoned", WorkflowID: tombstonedID})
	seedBackup(t, repo, "hidden-helper", archive.Metadata{Name: "Hidden Helper", WorkflowID: hiddenID})
	seedBackup(t, repo, "unselected", archive.Metadata{Name: "Unselected", WorkflowID: unselectedID})
	return repo
}

func TestReconcileMarksDeletedAndConflictButNotHiddenOrUnselected(t *testing.T) {
	repo := seedStandardRepo(t)
	svc := newTestService(t, repo, openFixtureLibrary(t, standardLibrary))

	backUp(t, svc, originalID, conflictID)
	changes := reconcile(t, svc)

	for _, slug := range []string{"gone", "tombstoned"} {
		if got := readMeta(t, repo, slug).ShortcutStatus(); got != shortcuts.StatusDeleted {
			t.Errorf("%s status = %q, want deleted", slug, got)
		}
		if c := changes[slug]; c.From != shortcuts.StatusActive || c.To != shortcuts.StatusDeleted {
			t.Errorf("%s change = %+v, want active -> deleted", slug, c)
		}
	}
	for _, slug := range []string{"hidden-helper", "unselected", "get-apple-app"} {
		if got := readMeta(t, repo, slug).ShortcutStatus(); got != shortcuts.StatusActive {
			t.Errorf("%s status = %q, want active", slug, got)
		}
	}

	conflict := readMeta(t, repo, "get-app-from-input")
	if conflict.ShortcutStatus() != shortcuts.StatusConflict {
		t.Errorf("conflict copy status = %q, want conflict", conflict.Status)
	}
	if want := (shortcuts.Original{WorkflowID: originalID, Name: "get_apple_app"}); conflict.Original() != want {
		t.Errorf("conflict_of = %+v, want %+v", conflict.Original(), want)
	}
	if len(changes) != 2 {
		t.Errorf("changes = %+v, want only the two deletions", changes)
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	repo := seedStandardRepo(t)
	lib := openFixtureLibrary(t, standardLibrary)
	first := newTestService(t, repo, lib)
	backUp(t, first, originalID, conflictID)
	reconcile(t, first)

	stamps := map[string]time.Time{}
	contents := map[string]string{}
	past := time.Now().Add(-time.Hour)
	entries, _ := filepath.Glob(filepath.Join(repo, ShortcutsDir, "*", MetadataFileName))
	for _, path := range entries {
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(path)
		data, _ := os.ReadFile(path)
		stamps[path], contents[path] = info.ModTime(), string(data)
	}

	second := newTestService(t, repo, lib)
	backUp(t, second, originalID, conflictID)
	if changes := reconcile(t, second); len(changes) != 0 {
		t.Errorf("second run changes = %+v, want none", changes)
	}
	for path, stamp := range stamps {
		info, _ := os.Stat(path)
		data, _ := os.ReadFile(path)
		if !info.ModTime().Equal(stamp) || string(data) != contents[path] {
			t.Errorf("%s was rewritten on an unchanged run", path)
		}
	}
	for _, p := range second.TouchedPaths() {
		if filepath.Base(filepath.Dir(p)) == "gone" {
			t.Errorf("unchanged deleted shortcut was staged: %s", p)
		}
	}
}

func TestReconcileClearsDeletedWhenAShortcutReappears(t *testing.T) {
	repo := t.TempDir()
	seedBackup(t, repo, "unselected", archive.Metadata{Name: "Unselected", WorkflowID: unselectedID, Status: "deleted"})
	seedBackup(t, repo, "get-apple-app", archive.Metadata{Name: "get_apple_app", WorkflowID: originalID, Status: "deleted"})
	svc := newTestService(t, repo, openFixtureLibrary(t, standardLibrary))

	backUp(t, svc, originalID)
	changes := reconcile(t, svc)

	for _, slug := range []string{"unselected", "get-apple-app"} {
		raw, err := os.ReadFile(filepath.Join(repo, ShortcutsDir, slug, MetadataFileName))
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if _, present := fields["status"]; present {
			t.Errorf("%s still has a status after reappearing:\n%s", slug, raw)
		}
		if c := changes[slug]; !c.Restored() {
			t.Errorf("%s change = %+v, want a restoration", slug, c)
		}
	}
}

func TestANewShortcutDoesNotTakeOverADeletedShortcutsDirectory(t *testing.T) {
	repo := t.TempDir()
	seedBackup(t, repo, "foo", archive.Metadata{Name: "Foo", WorkflowID: goneID})
	before, _ := os.ReadFile(filepath.Join(repo, ShortcutsDir, "foo", SourceFileName))

	svc := newTestService(t, repo, openFixtureLibrary(t, []fixtureRow{{pk: 1, name: "Foo", id: newcomerID}}))
	backUp(t, svc, newcomerID)
	reconcile(t, svc)

	if got := readMeta(t, repo, "foo-55555555").WorkflowID; got != newcomerID {
		t.Errorf("newcomer landed in a directory owned by %q", got)
	}
	old := readMeta(t, repo, "foo")
	if old.WorkflowID != goneID || old.ShortcutStatus() != shortcuts.StatusDeleted {
		t.Errorf("deleted shortcut's metadata = %+v, want its own id marked deleted", old)
	}
	if after, _ := os.ReadFile(filepath.Join(repo, ShortcutsDir, "foo", SourceFileName)); string(after) != string(before) {
		t.Error("the deleted shortcut's shortcut.json was overwritten")
	}
}

func TestResolveSlugsAgainstKeepsAnExistingDirectory(t *testing.T) {
	survivor := shortcut("Summarize PDF", "AAAAAAAA-0000-0000-0000-000000000001")
	owners := map[string]string{
		"summarize-pdf-aaaaaaaa": survivor.WorkflowID,
		"summarize-pdf-bbbbbbbb": "BBBBBBBB-0000-0000-0000-000000000002",
	}
	if got := ResolveSlugsAgainst([]shortcuts.Shortcut{survivor}, owners)[survivor.WorkflowID]; got != "summarize-pdf-aaaaaaaa" {
		t.Errorf("survivor of a collision moved to %q, want its existing directory", got)
	}

	plain := shortcut("Encode URL", "44434252-BEA5-4DA6-BC72-949BD054B2E3")
	if got := ResolveSlugsAgainst([]shortcuts.Shortcut{plain}, owners)[plain.WorkflowID]; got != "encode-url" {
		t.Errorf("unclaimed slug = %q, want encode-url", got)
	}
}

func TestBuildSiteCarriesStatusOntoTheWorkflow(t *testing.T) {
	repo := t.TempDir()
	seedBackup(t, repo, "gone", archive.Metadata{Name: "Gone", WorkflowID: goneID, Status: "deleted"})
	seedBackup(t, repo, "copy", archive.Metadata{
		Name: "Copy", WorkflowID: conflictID, Status: "conflict",
		ConflictOf: &archive.Origin{WorkflowID: originalID, Name: "get_apple_app"},
	})

	entries, err := newTestService(t, repo, nil).repoEntries()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*shortcuts.Workflow{}
	for _, e := range entries {
		got[e.Slug] = e.Workflow
	}
	if got["gone"].Status != shortcuts.StatusDeleted {
		t.Errorf("gone Status = %q, want deleted", got["gone"].Status)
	}
	if w := got["copy"]; w.Status != shortcuts.StatusConflict || w.ConflictOf.WorkflowID != originalID {
		t.Errorf("copy = %q %+v, want a conflict of %s", w.Status, w.ConflictOf, originalID)
	}
}
