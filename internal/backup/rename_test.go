package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
)

const renamedID = "66666666-0000-0000-0000-000000000006"

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func seedRenamedShortcut(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	first := newTestService(t, repo, openFixtureLibrary(t, []fixtureRow{{pk: 1, name: "Foo", id: renamedID}}))
	backUp(t, first, renamedID)
	if err := os.WriteFile(filepath.Join(repo, ShortcutsDir, "foo", "Foo.shortcut"), []byte("signed bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedBackup(t, repo, "gone", archive.Metadata{Name: "Gone", WorkflowID: goneID})
	return repo
}

func signingService(t *testing.T, repo string, rows []fixtureRow) *Service {
	t.Helper()
	svc := newTestService(t, repo, openFixtureLibrary(t, rows))
	svc.cfg.IncludeSigned = true
	return svc
}

func TestARenamedShortcutMovesItsDirectoryAndKeepsItsSignedFile(t *testing.T) {
	repo := seedRenamedShortcut(t)
	renamed := []fixtureRow{{pk: 1, name: "Bar", id: renamedID}}

	svc := signingService(t, repo, renamed)
	backUp(t, svc, renamedID)
	reconcile(t, svc)

	root := filepath.Join(repo, ShortcutsDir)
	if exists(filepath.Join(root, "foo")) {
		t.Error("the old directory is still there")
	}
	signed, err := os.ReadFile(filepath.Join(root, "bar", "Bar.shortcut"))
	if err != nil || string(signed) != "signed bytes" {
		t.Errorf("Bar.shortcut = %q, %v; want the existing signed bytes carried over", signed, err)
	}
	if exists(filepath.Join(root, "bar", "Foo.shortcut")) {
		t.Error("the signed file under the old name was left behind")
	}
	if got := readMeta(t, repo, "bar").Name; got != "Bar" {
		t.Errorf("metadata name = %q, want Bar", got)
	}
	if got := readMeta(t, repo, "gone"); got.WorkflowID != goneID || got.ShortcutStatus() != "deleted" {
		t.Errorf("deleted shortcut = %+v, want it kept and marked deleted", got)
	}

	renames := svc.Renames()
	if len(renames) != 1 || renames[0].String() != "Foo → Bar" || renames[0].FromSlug != "foo" || renames[0].ToSlug != "bar" {
		t.Errorf("renames = %+v, want Foo → Bar", renames)
	}
	removed := map[string]bool{}
	for _, p := range svc.RemovedPaths() {
		removed[p] = true
	}
	for _, want := range []string{filepath.Join(root, "foo"), filepath.Join(root, "bar", "Foo.shortcut")} {
		if !removed[want] {
			t.Errorf("RemovedPaths() = %v, missing %s", svc.RemovedPaths(), want)
		}
	}

	again := signingService(t, repo, renamed)
	backUp(t, again, renamedID)
	if changes := reconcile(t, again); len(changes) != 0 || len(again.Renames()) != 0 || len(again.RemovedPaths()) != 0 {
		t.Errorf("second run changes=%v renames=%v removed=%v, want nothing", changes, again.Renames(), again.RemovedPaths())
	}
}

func TestStaleDuplicatesOfAnUnselectedShortcutAreRemoved(t *testing.T) {
	repo := t.TempDir()
	seedBackup(t, repo, "unselected", archive.Metadata{Name: "Unselected", WorkflowID: unselectedID})
	seedBackup(t, repo, "old-unselected-name", archive.Metadata{Name: "Old Unselected Name", WorkflowID: unselectedID})
	seedBackup(t, repo, "hidden-helper-old", archive.Metadata{Name: "Hidden Helper Old", WorkflowID: hiddenID})
	seedBackup(t, repo, "gone", archive.Metadata{Name: "Gone", WorkflowID: goneID})
	seedBackup(t, repo, "no-id", archive.Metadata{Name: "No Id"})

	svc := newTestService(t, repo, openFixtureLibrary(t, standardLibrary))
	backUp(t, svc, originalID)
	reconcile(t, svc)

	root := filepath.Join(repo, ShortcutsDir)
	if exists(filepath.Join(root, "old-unselected-name")) {
		t.Error("stale duplicate of a live shortcut was kept")
	}
	for _, kept := range []string{"unselected", "hidden-helper-old", "gone", "no-id"} {
		if !exists(filepath.Join(root, kept)) {
			t.Errorf("%s was removed", kept)
		}
	}
	if renames := svc.Renames(); len(renames) != 1 || renames[0].String() != "Old Unselected Name → Unselected" {
		t.Errorf("renames = %+v, want the one cleaned duplicate", renames)
	}
}
