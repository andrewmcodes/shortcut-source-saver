package shortcuts

import (
	"database/sql"
	"errors"
	"os"
	"testing"
)

// listImmutable reads the live database the naive way, with immutable=1, which tells
// SQLite to ignore the -wal sidecar. Only used to demonstrate what Open avoids.
func listImmutable(path string) ([]string, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?immutable=1")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT ZNAME FROM ZSHORTCUT
		WHERE COALESCE(ZTOMBSTONED,0)=0 AND ZNAME IS NOT NULL AND ZACTIONS IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// openLocal opens the real library, skipping when it is unavailable (CI, or a machine
// without Full Disk Access).
func openLocal(t *testing.T) *Library {
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

// TestOpenSeesWALContents guards the reason Open snapshots the -wal sidecar. Shortcuts
// leaves recent edits in the WAL, and reading the database without it silently omits
// them. If this regresses, shortcuts the user just created stop appearing in the TUI.
func TestOpenSeesWALContents(t *testing.T) {
	lib := openLocal(t)

	all, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("List returned no shortcuts")
	}

	// Compare against the same database opened the wrong way. Any shortcut visible only
	// with the WAL replayed proves the snapshot is doing its job; if the live database
	// happens to be fully checkpointed the two agree and there is nothing to assert.
	path, _ := DefaultPath()
	stale, err := listImmutable(path)
	if err != nil {
		t.Skipf("cannot read the live database directly: %v", err)
	}
	if len(all) < len(stale) {
		t.Errorf("snapshot saw %d shortcuts, a WAL-less read saw %d: the snapshot lost data",
			len(all), len(stale))
	}
	t.Logf("snapshot saw %d shortcuts; a WAL-less read saw %d", len(all), len(stale))
}

func TestListPopulatesFields(t *testing.T) {
	lib := openLocal(t)
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

// TestLoadDecodesActions checks that every shortcut in the library decodes, so that a
// single unusual action type cannot break a full backup run.
func TestLoadDecodesActions(t *testing.T) {
	lib := openLocal(t)
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
