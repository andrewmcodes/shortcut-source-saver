package shortcuts

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openLocalLibrary(t *testing.T) *Library {
	t.Helper()
	path, err := DefaultPath()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no Shortcuts database at %s", path)
	}
	lib, err := Open("")
	if errors.Is(err, ErrNoAccess) {
		t.Skipf("no Full Disk Access: %v", err)
	}
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

const liveShortcutsQuery = `SELECT ZWORKFLOWID FROM ZSHORTCUT
	WHERE COALESCE(ZTOMBSTONED,0)=0 AND ZNAME IS NOT NULL AND ZACTIONS IS NOT NULL`

func workflowIDs(path, options string) (map[string]bool, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?"+options)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(liveShortcutsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func TestSnapshotMatchesAWALAwareReadOfTheLiveDatabase(t *testing.T) {
	lib := openLocalLibrary(t)

	all, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) == 0 {
		t.Skip("the local library has no shortcuts")
	}
	snapshot := map[string]bool{}
	for _, s := range all {
		snapshot[s.WorkflowID] = true
	}

	path, _ := DefaultPath()
	live, err := workflowIDs(path, "mode=ro")
	if err != nil {
		t.Skipf("cannot read the live database directly: %v", err)
	}
	for id := range live {
		if !snapshot[id] {
			t.Errorf("shortcut %s is in the live database but missing from the snapshot", id)
		}
	}
	for id := range snapshot {
		if !live[id] {
			t.Errorf("shortcut %s is in the snapshot but not in the live database", id)
		}
	}

	if walless, err := workflowIDs(path, "immutable=1"); err == nil {
		t.Logf("snapshot %d shortcuts, live %d, a WAL-ignoring read %d", len(snapshot), len(live), len(walless))
	}
}

func TestListPopulatesEveryField(t *testing.T) {
	lib := openLocalLibrary(t)
	all, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, s := range all {
		if s.Name == "" {
			t.Error("shortcut with an empty name")
		}
		if len(s.WorkflowID) != 36 {
			t.Errorf("%s: workflow id %q is not a UUID", s.Name, s.WorkflowID)
		}
		if s.Created.IsZero() {
			t.Errorf("%s: zero creation date", s.Name)
		}
	}
}

func TestEveryShortcutInTheLibraryDecodes(t *testing.T) {
	lib := openLocalLibrary(t)
	all, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var actions int
	for _, s := range all {
		w, err := lib.Load(s)
		if err != nil {
			t.Errorf("Load(%q): %v", s.Name, err)
			continue
		}
		actions += len(w.Actions)
		for i, a := range w.Actions {
			if _, ok := a.(map[string]any); !ok {
				t.Errorf("%s: action %d decoded as %T, want a dictionary", s.Name, i, a)
			}
		}
	}
	t.Logf("decoded %d shortcuts, %d actions total", len(all), actions)
}

func TestCoreDataTimeConvertsFromTheReferenceEpoch(t *testing.T) {
	got := coreDataTime(sql.NullFloat64{Float64: 0, Valid: true}).UTC()
	if !got.Equal(coreDataEpoch) {
		t.Errorf("zero timestamp = %s, want %s", got, coreDataEpoch)
	}
	if zero := coreDataTime(sql.NullFloat64{}); !zero.IsZero() {
		t.Errorf("NULL timestamp = %s, want the zero time", zero)
	}
}

func fixtureLibrary(t *testing.T, workflowID any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Shortcuts.sqlite")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE ZSHORTCUT (Z_PK INTEGER PRIMARY KEY, ZNAME TEXT, ZWORKFLOWID TEXT, ZACTIONS INTEGER,
			ZICON INTEGER, ZTOMBSTONED INTEGER, ZACTIONCOUNT INTEGER, ZCREATIONDATE REAL,
			ZMODIFICATIONDATE REAL, ZHIDDENFROMLIBRARYANDSYNC INTEGER)`,
		`CREATE TABLE ZSHORTCUTACTIONS (Z_PK INTEGER PRIMARY KEY, ZDATA BLOB)`,
		`CREATE TABLE ZSHORTCUTICON (Z_PK INTEGER PRIMARY KEY, ZGLYPHNUMBER INTEGER, ZBACKGROUNDCOLORVALUE INTEGER)`,
		`INSERT INTO ZSHORTCUTACTIONS (Z_PK, ZDATA) VALUES (1, NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO ZSHORTCUT (Z_PK, ZNAME, ZWORKFLOWID, ZACTIONS) VALUES (1, 'Orphan', ?, 1)`,
		workflowID); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestListNamesAShortcutWithNoWorkflowIdentifier(t *testing.T) {
	lib, err := Open(fixtureLibrary(t, nil))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer lib.Close()

	_, err = lib.List()
	if err == nil || !strings.Contains(err.Error(), `"Orphan"`) {
		t.Fatalf("List error = %v, want one naming the shortcut", err)
	}
}

func execAll(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func listFixture(t *testing.T, path string) map[string]Shortcut {
	t.Helper()
	lib, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer lib.Close()
	all, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]Shortcut{}
	for _, s := range all {
		byName[s.Name] = s
	}
	return byName
}

func TestListMarksSyncConflictCopiesWithTheirOriginal(t *testing.T) {
	path := fixtureLibrary(t, "ORPHAN-ID")
	execAll(t, path,
		`ALTER TABLE ZSHORTCUT ADD COLUMN ZCONFLICTOF INTEGER`,
		`INSERT INTO ZSHORTCUT (Z_PK, ZNAME, ZWORKFLOWID, ZACTIONS) VALUES (2, 'get_apple_app', 'ORIGINAL-ID', 1)`,
		`INSERT INTO ZSHORTCUT (Z_PK, ZNAME, ZWORKFLOWID, ZACTIONS, ZCONFLICTOF) VALUES (3, '_Get App from Input', 'COPY-ID', 1, 2)`,
		`INSERT INTO ZSHORTCUT (Z_PK, ZNAME, ZWORKFLOWID, ZACTIONS, ZCONFLICTOF) VALUES (4, 'Dangling', 'DANGLING-ID', 1, 99)`,
	)

	byName := listFixture(t, path)

	conflict := byName["_Get App from Input"]
	if conflict.Status != StatusConflict {
		t.Errorf("conflict copy Status = %q, want %q", conflict.Status, StatusConflict)
	}
	if want := (Original{WorkflowID: "ORIGINAL-ID", Name: "get_apple_app"}); conflict.ConflictOf != want {
		t.Errorf("ConflictOf = %+v, want %+v", conflict.ConflictOf, want)
	}
	if original := byName["get_apple_app"]; original.Status != StatusActive || original.ConflictOf != (Original{}) {
		t.Errorf("original = %q %+v, want active with no ConflictOf", original.Status, original.ConflictOf)
	}
	if dangling := byName["Dangling"]; dangling.Status != StatusConflict || dangling.ConflictOf != (Original{}) {
		t.Errorf("dangling conflict = %q %+v, want a conflict with an unknown original", dangling.Status, dangling.ConflictOf)
	}
}

func TestListWorksWithoutTheConflictColumn(t *testing.T) {
	byName := listFixture(t, fixtureLibrary(t, "ORPHAN-ID"))
	if s, ok := byName["Orphan"]; !ok || s.Status != StatusActive {
		t.Errorf("List without ZCONFLICTOF = %+v, want the shortcut listed as active", byName)
	}
}
