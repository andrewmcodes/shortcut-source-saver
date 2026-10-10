package export

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

type fakeSource struct {
	shortcuts []shortcuts.Shortcut
	workflows map[string]*shortcuts.Workflow
	failures  map[string]error
}

func (f fakeSource) List() ([]shortcuts.Shortcut, error) { return f.shortcuts, nil }

func (f fakeSource) Load(sc shortcuts.Shortcut) (*shortcuts.Workflow, error) {
	if err := f.failures[sc.WorkflowID]; err != nil {
		return nil, err
	}
	return f.workflows[sc.WorkflowID], nil
}

func newFakeSource(scs ...shortcuts.Shortcut) fakeSource {
	src := fakeSource{workflows: map[string]*shortcuts.Workflow{}, failures: map[string]error{}}
	for _, sc := range scs {
		src.shortcuts = append(src.shortcuts, sc)
		src.workflows[sc.WorkflowID] = &shortcuts.Workflow{
			Shortcut: sc,
			Actions: []any{map[string]any{
				"WFWorkflowActionIdentifier": "is.workflow.actions.comment",
				"WFWorkflowActionParameters": map[string]any{"Blob": []byte{0xde, 0xad, 0xbe, 0xef}},
			}},
		}
	}
	return src
}

func TestCreateExclusiveNeverOverwrites(t *testing.T) {
	target := filepath.Join(t.TempDir(), "out.zip")
	if err := os.WriteFile(target, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	called := false
	err := CreateExclusive(target, func(io.Writer) error { called = true; return nil })

	if !errors.Is(err, ErrOutputExists) {
		t.Fatalf("err = %v, want ErrOutputExists", err)
	}
	if called {
		t.Error("write ran even though the output already existed")
	}
	if got, _ := os.ReadFile(target); string(got) != "keep me" {
		t.Errorf("existing file was modified: %q", got)
	}
}

func TestCreateExclusiveRemovesPartialFileOnFailure(t *testing.T) {
	target := filepath.Join(t.TempDir(), "out.zip")
	boom := errors.New("boom")

	err := CreateExclusive(target, func(w io.Writer) error {
		if _, err := w.Write([]byte("partial")); err != nil {
			return err
		}
		return boom
	})

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("partial output left behind: %v", statErr)
	}
}

func TestExportedJSONMatchesBackupBytes(t *testing.T) {
	src := newFakeSource(
		shortcuts.Shortcut{Name: "Summarize PDF", WorkflowID: "AAAAAAAA-0000-0000-0000-000000000001"},
		shortcuts.Shortcut{Name: "Summarize PDF", WorkflowID: "BBBBBBBB-0000-0000-0000-000000000002"},
		shortcuts.Shortcut{Name: "Secret Stash", WorkflowID: "CCCCCCCC-0000-0000-0000-000000000003", Hidden: true},
	)

	files, count, err := Collect(src, Options{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (hidden shortcut excluded)", count)
	}

	var buf bytes.Buffer
	if err := WriteZip(&buf, files); err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("reading zip: %v", err)
	}

	want, err := archive.CanonicalJSON(archive.BuildWorkflow(src.workflows["AAAAAAAA-0000-0000-0000-000000000001"]))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}

	got, ok := contents["shortcuts/summarize-pdf-aaaaaaaa/shortcut.json"]
	if !ok {
		t.Fatalf("missing disambiguated entry; zip holds %v", keys(contents))
	}
	if !bytes.Equal(got, want) {
		t.Errorf("exported JSON differs from backup canonical JSON:\n%s\nwant:\n%s", got, want)
	}
	if !bytes.Contains(got, []byte(`"__value__": "3q2+7w=="`)) {
		t.Errorf("data value not tagged as base64:\n%s", got)
	}
	if _, ok := contents["shortcuts/summarize-pdf-bbbbbbbb/metadata.json"]; !ok {
		t.Errorf("missing metadata for the second shortcut; zip holds %v", keys(contents))
	}
	if len(contents) != 4 {
		t.Errorf("zip holds %d entries, want 4: %v", len(contents), keys(contents))
	}
}

func TestCollectNamesEveryFailingShortcut(t *testing.T) {
	src := newFakeSource(
		shortcuts.Shortcut{Name: "Good", WorkflowID: "11111111-0000-0000-0000-000000000001"},
		shortcuts.Shortcut{Name: "Broken One", WorkflowID: "22222222-0000-0000-0000-000000000002"},
		shortcuts.Shortcut{Name: "Broken Two", WorkflowID: "33333333-0000-0000-0000-000000000003"},
	)
	src.failures["22222222-0000-0000-0000-000000000002"] = errors.New("expected a plist array")
	src.failures["33333333-0000-0000-0000-000000000003"] = errors.New("has no action data")

	files, _, err := Collect(src, Options{})

	var collectErr *CollectError
	if !errors.As(err, &collectErr) {
		t.Fatalf("err = %v, want *CollectError", err)
	}
	if len(collectErr.Failures) != 2 {
		t.Errorf("got %d failures, want 2", len(collectErr.Failures))
	}
	for _, name := range []string{"Broken One", "Broken Two"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not name %q: %v", name, err)
		}
	}
	if files != nil {
		t.Errorf("Collect returned %d files alongside a failure", len(files))
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
