package integration

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

const referenceShortcutName = "Shortcut Source Tool"

func referenceExportPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", ".ai", "shortcut_source_tool.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("reference export not present at %s", path)
	}
	return path
}

func openLibrary(t *testing.T) *shortcuts.Library {
	t.Helper()
	path, err := shortcuts.DefaultPath()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no Shortcuts database at %s", path)
	}
	lib, err := shortcuts.Open("")
	if err != nil {
		t.Skipf("cannot open the Shortcuts library: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalizeNumbers(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeNumbers(val)
		}
		return out
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<53 {
			return int64(t)
		}
		return t
	case json.Number:
		return t.String()
	default:
		return v
	}
}

func roundTripThroughJSON(t *testing.T, v any) any {
	t.Helper()
	encoded, err := archive.CanonicalJSON(v)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding canonical JSON: %v", err)
	}
	return normalizeNumbers(decoded)
}

func TestExtractedActionsMatchTheReferenceExport(t *testing.T) {
	path := referenceExportPath(t)
	lib := openLibrary(t)

	matches, err := lib.FindByName(referenceShortcutName)
	if err != nil {
		t.Skipf("%q is not installed: %v", referenceShortcutName, err)
	}
	w, err := lib.Load(matches[0])
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading reference export: %v", err)
	}
	var reference map[string]any
	if err := json.Unmarshal(raw, &reference); err != nil {
		t.Fatalf("parsing reference export: %v", err)
	}

	wantActions, ok := reference["WFWorkflowActions"].([]any)
	if !ok {
		t.Fatal("reference export has no WFWorkflowActions array")
	}
	if len(w.Actions) != len(wantActions) {
		t.Fatalf("extracted %d actions, reference has %d", len(w.Actions), len(wantActions))
	}

	got := roundTripThroughJSON(t, w.Actions)
	want := normalizeNumbers(wantActions)

	gotList := got.([]any)
	wantList := want.([]any)
	mismatches := 0
	for i := range gotList {
		if !reflect.DeepEqual(gotList[i], wantList[i]) {
			mismatches++
			if mismatches <= 3 {
				t.Errorf("action %d differs from the reference export:\n got: %#v\nwant: %#v",
					i, gotList[i], wantList[i])
			}
		}
	}
	if mismatches > 3 {
		t.Errorf("%d actions differ in total", mismatches)
	}
	t.Logf("compared %d actions, %d mismatches", len(gotList), mismatches)
}

func TestBuiltWorkflowMatchesTheReferenceTopLevelKeys(t *testing.T) {
	path := referenceExportPath(t)
	lib := openLibrary(t)

	matches, err := lib.FindByName(referenceShortcutName)
	if err != nil {
		t.Skipf("%q is not installed: %v", referenceShortcutName, err)
	}
	w, err := lib.Load(matches[0])
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	raw, _ := os.ReadFile(path)
	var reference map[string]any
	if err := json.Unmarshal(raw, &reference); err != nil {
		t.Fatalf("parsing reference export: %v", err)
	}

	built := roundTripThroughJSON(t, archive.BuildWorkflow(w)).(map[string]any)

	for _, key := range []string{
		"WFWorkflowClientVersion",
		"WFWorkflowMinimumClientVersion",
		"WFWorkflowMinimumClientVersionString",
		"WFWorkflowIcon",
		"WFWorkflowInputContentItemClasses",
		"WFWorkflowOutputContentItemClasses",
		"WFWorkflowImportQuestions",
		"WFWorkflowHasOutputFallback",
		"WFWorkflowHasShortcutInputVariables",
		"WFWorkflowTypes",
	} {
		want, present := reference[key]
		if !present {
			t.Errorf("reference export is missing %s", key)
			continue
		}
		got, present := built[key]
		if !present {
			t.Errorf("built workflow is missing %s", key)
			continue
		}
		if !reflect.DeepEqual(got, normalizeNumbers(want)) {
			t.Errorf("%s = %#v, reference has %#v", key, got, normalizeNumbers(want))
		}
	}
}

func TestCanonicalJSONIsStableAcrossRuns(t *testing.T) {
	lib := openLibrary(t)
	all, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) == 0 {
		t.Skip("empty library")
	}

	for _, s := range all {
		w, err := lib.Load(s)
		if err != nil {
			t.Errorf("Load(%q): %v", s.Name, err)
			continue
		}
		built := archive.BuildWorkflow(w)
		first, err := archive.CanonicalJSON(built)
		if err != nil {
			t.Errorf("CanonicalJSON(%q): %v", s.Name, err)
			continue
		}
		second, err := archive.CanonicalJSON(archive.BuildWorkflow(w))
		if err != nil {
			t.Errorf("CanonicalJSON(%q) second pass: %v", s.Name, err)
			continue
		}
		if string(first) != string(second) {
			t.Errorf("%q: canonical JSON is not stable across runs", s.Name)
		}
		var back any
		if err := json.Unmarshal(first, &back); err != nil {
			t.Errorf("%q: canonical JSON does not parse: %v", s.Name, err)
		}
	}
}
