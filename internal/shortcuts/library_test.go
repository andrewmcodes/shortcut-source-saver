package shortcuts

import (
	"database/sql"
	"errors"
	"os"
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

func listIgnoringWAL(path string) ([]string, error) {
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

func TestSnapshotDoesNotLoseShortcutsLeftInTheWAL(t *testing.T) {
	lib := openLocalLibrary(t)

	all, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("List returned no shortcuts")
	}

	path, _ := DefaultPath()
	stale, err := listIgnoringWAL(path)
	if err != nil {
		t.Skipf("cannot read the live database directly: %v", err)
	}
	if len(all) < len(stale) {
		t.Errorf("snapshot saw %d shortcuts, a WAL-less read saw %d: the snapshot lost data",
			len(all), len(stale))
	}
	t.Logf("snapshot saw %d shortcuts; a WAL-less read saw %d", len(all), len(stale))
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
