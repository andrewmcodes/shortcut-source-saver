package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, root, rel, contents string) {
	t.Helper()
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInspectDir(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  DirKind
	}{
		"only finder and git metadata": {map[string]string{".DS_Store": "", ".git/HEAD": "ref"}, DirEmpty},
		"unrelated files":              {map[string]string{"notes.txt": "mine"}, DirForeign},
		"sss backup with extras": {map[string]string{
			"shortcuts/a/metadata.json": `{"name":"A","workflow_id":"AAAA"}`,
			"README.md":                 "mine",
		}, DirBackup},
		"a shortcuts folder sss did not write": {map[string]string{
			"shortcuts/a/metadata.json": `{"title":"something else"}`,
			"shortcuts/b/notes.txt":     "mine",
		}, DirForeign},
		"shortcuts is a file": {map[string]string{"shortcuts": "mine"}, DirForeign},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, contents := range tc.files {
				put(t, dir, rel, contents)
			}
			got, err := InspectDir(dir)
			if err != nil {
				t.Fatalf("InspectDir: %v", err)
			}
			if got != tc.want {
				t.Errorf("InspectDir = %d, want %d", got, tc.want)
			}
		})
	}

	if got, err := InspectDir(filepath.Join(t.TempDir(), "missing")); err != nil || got != DirEmpty {
		t.Errorf("InspectDir(missing) = %d, %v; want DirEmpty", got, err)
	}
}
