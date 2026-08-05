package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
	"howett.net/plist"
)

func TestSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "Morning Routine", "morning-routine"},
		{"already a slug", "morning-routine", "morning-routine"},
		{"slash", "Work/Home Toggle", "work-home-toggle"},
		{"leading emoji", "🚀 Deploy Podia", "deploy-podia"},
		{"trailing emoji", "Deploy 🚀", "deploy"},
		{"emoji only", "🚀🎉", fallbackSlug},
		{"non-latin script", "日本語", fallbackSlug},
		{"empty", "", fallbackSlug},
		{"whitespace only", "   \t\n", fallbackSlug},
		{"punctuation collapses", "What?! Is --- this…", "what-is-this"},
		{"accents dropped", "Café Order", "caf-order"},
		{"digits kept", "Timer 25 min", "timer-25-min"},
		{"underscores separate", "my_shortcut_name", "my-shortcut-name"},
		{"mixed case", "MyShortCUT", "myshortcut"},
		{"surrounding punctuation", "--Hello--", "hello"},
		{"long name truncated", strings.Repeat("ab ", 40), strings.Repeat("ab-", 26) + "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slug(tt.in)
			if got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if got == "" {
				t.Error("Slug returned an empty string")
			}
			if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
				t.Errorf("Slug(%q) = %q, want no leading or trailing hyphen", tt.in, got)
			}
			if strings.Contains(got, "--") {
				t.Errorf("Slug(%q) = %q, want no repeated hyphens", tt.in, got)
			}
			if len(got) > maxSlugLength {
				t.Errorf("Slug(%q) is %d bytes, want at most %d", tt.in, len(got), maxSlugLength)
			}
		})
	}
}

func TestHexColor(t *testing.T) {
	tests := []struct {
		name  string
		color uint32
		want  string
	}{
		{"yellow", 0xFFD426FF, "#FFD426"},
		{"black", 0x000000FF, "#000000"},
		{"zero", 0, "#000000"},
		{"low byte is alpha, not blue", 0x11223344, "#112233"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HexColor(tt.color); got != tt.want {
				t.Errorf("HexColor(%#x) = %q, want %q", tt.color, got, tt.want)
			}
		})
	}
}

func TestBuildWorkflowIconColorIsSignedInt32(t *testing.T) {
	wf := BuildWorkflow(&shortcuts.Workflow{
		Shortcut: shortcuts.Shortcut{Icon: shortcuts.Icon{Glyph: 59511, Color: 0xFFD426FF}},
	})
	icon, ok := wf["WFWorkflowIcon"].(map[string]any)
	if !ok {
		t.Fatalf("WFWorkflowIcon is %T, want a dictionary", wf["WFWorkflowIcon"])
	}
	got, ok := icon["WFWorkflowIconStartColor"].(int32)
	if !ok {
		t.Fatalf("WFWorkflowIconStartColor is %T, want int32", icon["WFWorkflowIconStartColor"])
	}
	if got != -2873601 {
		t.Errorf("WFWorkflowIconStartColor = %d, want -2873601", got)
	}
	if icon["WFWorkflowIconGlyphNumber"] != int64(59511) {
		t.Errorf("WFWorkflowIconGlyphNumber = %v, want 59511", icon["WFWorkflowIconGlyphNumber"])
	}
}

func TestWorkflowTypes(t *testing.T) {
	tests := []struct {
		name string
		w    shortcuts.Workflow
		want []string
	}{
		{
			name: "widget only",
			w:    shortcuts.Workflow{},
			want: []string{"NCWidget"},
		},
		{
			name: "hidden from widget and search",
			w:    shortcuts.Workflow{HiddenFromWidget: true},
			want: []string{},
		},
		{
			name: "accepts input",
			w:    shortcuts.Workflow{InputContentItemClasses: []any{"WFStringContentItem"}},
			want: []string{"ActionExtension", "NCWidget"},
		},
		{
			name: "all three in order",
			w: shortcuts.Workflow{
				InputContentItemClasses: []any{"WFStringContentItem"},
				ShowInSearch:            true,
			},
			want: []string{"ActionExtension", "NCWidget", "WFWorkflowTypeShowInSearch"},
		},
		{
			name: "search only",
			w:    shortcuts.Workflow{HiddenFromWidget: true, ShowInSearch: true},
			want: []string{"WFWorkflowTypeShowInSearch"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WorkflowTypes(&tt.w)
			if len(got) != len(tt.want) {
				t.Fatalf("WorkflowTypes = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("WorkflowTypes = %v, want %v", got, tt.want)
				}
			}
			if bwt := BuildWorkflow(&tt.w)["WFWorkflowTypes"]; !equalStrings(bwt, tt.want) {
				t.Errorf("BuildWorkflow WFWorkflowTypes = %v, want %v", bwt, tt.want)
			}
		})
	}
}

func equalStrings(v any, want []string) bool {
	got, ok := v.([]string)
	if !ok || len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestBuildWorkflowMinimumClientVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    any
		present bool
	}{
		{"numeric", "900", 900, true},
		{"padded", " 1128 ", 1128, true},
		{"dotted is not an integer", "2.2.1", nil, false},
		{"empty", "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := BuildWorkflow(&shortcuts.Workflow{MinimumClientVersion: tt.version})
			if wf["WFWorkflowMinimumClientVersionString"] != tt.version {
				t.Errorf("WFWorkflowMinimumClientVersionString = %v, want %q",
					wf["WFWorkflowMinimumClientVersionString"], tt.version)
			}
			got, ok := wf["WFWorkflowMinimumClientVersion"]
			if ok != tt.present {
				t.Fatalf("WFWorkflowMinimumClientVersion present = %v, want %v", ok, tt.present)
			}
			if tt.present && got != tt.want {
				t.Errorf("WFWorkflowMinimumClientVersion = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildWorkflowPassesThroughArrays(t *testing.T) {
	w := &shortcuts.Workflow{
		Actions:                  []any{map[string]any{"WFWorkflowActionIdentifier": "is.workflow.actions.comment"}},
		InputContentItemClasses:  []any{"WFStringContentItem"},
		OutputContentItemClasses: []any{"WFImageContentItem"},
		ImportQuestions:          []any{},
		ClientVersion:            "3072.2.2",
		HasOutputFallback:        true,
	}
	wf := BuildWorkflow(w)

	for key, want := range map[string]any{
		"WFWorkflowClientVersion":             "3072.2.2",
		"WFWorkflowHasOutputFallback":         true,
		"WFWorkflowHasShortcutInputVariables": false,
	} {
		if wf[key] != want {
			t.Errorf("%s = %v, want %v", key, wf[key], want)
		}
	}
	if len(wf["WFWorkflowActions"].([]any)) != 1 {
		t.Errorf("WFWorkflowActions = %v, want the workflow's actions", wf["WFWorkflowActions"])
	}
	if surfaces, ok := wf["WFQuickActionSurfaces"].([]any); !ok || len(surfaces) != 0 {
		t.Errorf("WFQuickActionSurfaces = %v, want an empty array", wf["WFQuickActionSurfaces"])
	}
	for _, key := range []string{"WFWorkflowInputContentItemClasses", "WFWorkflowOutputContentItemClasses", "WFWorkflowImportQuestions"} {
		if _, ok := wf[key]; !ok {
			t.Errorf("missing key %s", key)
		}
	}
}

func TestCanonicalJSONDoesNotEscapeHTML(t *testing.T) {
	data, err := CanonicalJSON(map[string]any{"text": `a < b && c > d "quoted"`})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `a < b && c > d \"quoted\"`) {
		t.Errorf("HTML characters were escaped: %s", got)
	}
	for _, escape := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(got, escape) {
			t.Errorf("output contains the escape %s: %s", escape, got)
		}
	}
}

func TestCanonicalJSONShape(t *testing.T) {
	data, err := CanonicalJSON(map[string]any{"b": 1, "a": 2, "nested": map[string]any{"z": true, "y": false}})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	want := "{\n  \"a\": 2,\n  \"b\": 1,\n  \"nested\": {\n    \"y\": false,\n    \"z\": true\n  }\n}\n"
	if string(data) != want {
		t.Errorf("CanonicalJSON =\n%q\nwant\n%q", data, want)
	}
	if bytes.HasSuffix(data, []byte("\n\n")) {
		t.Error("output has more than one trailing newline")
	}
}

func TestCanonicalJSONIsDeterministic(t *testing.T) {
	wf := map[string]any{
		"WFWorkflowActions": []any{
			map[string]any{"a": uint64(1), "b": uint64(2), "c": uint64(3), "d": "four", "e": true},
			map[string]any{"z": uint64(26), "y": uint64(25), "x": uint64(24)},
		},
	}
	first, err := CanonicalJSON(wf)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := CanonicalJSON(wf)
		if err != nil {
			t.Fatalf("CanonicalJSON: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("run %d differs:\n%s\n%s", i, first, again)
		}
	}
}

func TestCanonicalizeTagsPlistScalars(t *testing.T) {
	when := time.Date(2024, time.March, 1, 12, 0, 0, 0, time.UTC)
	data, err := CanonicalJSON(map[string]any{
		"blob":   []byte{0xDE, 0xAD, 0xBE, 0xEF},
		"date":   when,
		"count":  uint64(42),
		"nested": []any{[]byte("hi"), when, uint64(7)},
		"huge":   uint64(1) << 63,
	})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("round trip: %v\n%s", err, data)
	}

	blob := got["blob"].(map[string]any)
	if blob[TypeKey] != TypeData || blob[ValueKey] != "3q2+7w==" {
		t.Errorf("blob = %v, want tagged base64 data", blob)
	}
	date := got["date"].(map[string]any)
	if date[TypeKey] != TypeDate || date[ValueKey] != float64(when.Unix()) {
		t.Errorf("date = %v, want tagged unix seconds", date)
	}
	if got["count"] != float64(42) {
		t.Errorf("count = %v, want a plain number", got["count"])
	}
	if got["huge"] != float64(uint64(1)<<63) {
		t.Errorf("huge = %v, want a plain number", got["huge"])
	}
	nested := got["nested"].([]any)
	if len(nested) != 3 {
		t.Fatalf("nested = %v, want three elements", nested)
	}
	if nested[0].(map[string]any)[TypeKey] != TypeData {
		t.Errorf("nested data was not tagged: %v", nested[0])
	}
	if nested[1].(map[string]any)[TypeKey] != TypeDate {
		t.Errorf("nested date was not tagged: %v", nested[1])
	}
}

func TestCanonicalizeNamedTypesOverJSONSafeKinds(t *testing.T) {
	data, err := CanonicalJSON(map[string]any{
		"uid":     plist.UID(9),
		"strings": []string{"a", "b"},
		"ints":    []int64{1, 2},
	})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	want := "{\n  \"ints\": [\n    1,\n    2\n  ],\n  \"strings\": [\n    \"a\",\n    \"b\"\n  ],\n  \"uid\": 9\n}\n"
	if string(data) != want {
		t.Errorf("CanonicalJSON =\n%s\nwant\n%s", data, want)
	}
}

func TestWriteJSONChangeDetection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "shortcut.json")
	wf := map[string]any{"WFWorkflowActions": []any{"one"}}

	changed, err := WriteJSON(path, wf)
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !changed {
		t.Error("first write reported no change")
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	changed, err = WriteJSON(path, wf)
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if changed {
		t.Error("rewriting identical content reported a change")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("unchanged content was rewritten: the file's mtime moved")
	}

	wf["WFWorkflowActions"] = []any{"one", "two"}
	changed, err = WriteJSON(path, wf)
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !changed {
		t.Error("modified content reported no change")
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	want, err := CanonicalJSON(wf)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if !bytes.Equal(onDisk, want) {
		t.Errorf("file contents =\n%s\nwant\n%s", onDisk, want)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil {
		t.Fatalf("readdir: %v", err)
	} else if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the JSON file (temp files leaked)", len(entries))
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "anyone", want: ModeAnyone},
		{in: "people-who-know-me", want: ModePeopleWhoKnowMe},
		{in: "People-Who-Know-Me", want: ModePeopleWhoKnowMe},
		{in: " people-who-know-me ", want: ModePeopleWhoKnowMe},
		{in: "", want: ModeAnyone},
		{in: "nonsense", wantErr: true},
		{in: "people-who-know-mee", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseMode(tt.in)
		if tt.wantErr {
			if !errors.Is(err, ErrUnknownSignMode) {
				t.Errorf("ParseMode(%q) error = %v, want ErrUnknownSignMode", tt.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMode(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeModeFallsBackToTheRestrictiveMode(t *testing.T) {
	if got := NormalizeMode("nonsense"); got != ModePeopleWhoKnowMe {
		t.Errorf("NormalizeMode(%q) = %q, want the restrictive %q so a typo cannot widen who may import a shortcut",
			"nonsense", got, ModePeopleWhoKnowMe)
	}
	if got := NormalizeMode(""); got != ModeAnyone {
		t.Errorf("NormalizeMode(\"\") = %q, want the documented default %q", got, ModeAnyone)
	}
}

func TestSignRejectsAnUnknownMode(t *testing.T) {
	err := Sign(true, map[string]any{"WFWorkflowActions": []any{}},
		filepath.Join(t.TempDir(), "out.shortcut"), "nonsense")
	if !errors.Is(err, ErrUnknownSignMode) {
		t.Errorf("Sign() error = %v, want ErrUnknownSignMode", err)
	}
}

func TestSignSkipsWhenJSONUnchangedAndOutputExists(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "existing.shortcut")
	if err := os.WriteFile(out, []byte("previously signed"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Sign(false, map[string]any{"WFWorkflowActions": []any{}}, out, "anyone"); err != nil {
		t.Fatalf("Sign should have skipped, got %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "previously signed" {
		t.Errorf("existing file was rewritten: %q", got)
	}
}

func TestSignRunsWhenOutputMissing(t *testing.T) {
	if _, err := exec.LookPath("shortcuts"); err != nil {
		t.Skip("the shortcuts binary is unavailable")
	}
	out := filepath.Join(t.TempDir(), "signed", "test.shortcut")
	wf := BuildWorkflow(&shortcuts.Workflow{
		Actions: []any{map[string]any{
			"WFWorkflowActionIdentifier": "is.workflow.actions.comment",
			"WFWorkflowActionParameters": map[string]any{"WFCommentActionText": "hello <world> & co"},
		}},
		ClientVersion:        "3072.2.2",
		MinimumClientVersion: "900",
	})
	if err := Sign(false, wf, out, "anyone"); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("signed file is empty")
	}
}

func TestWriteMetadata(t *testing.T) {
	created := time.Date(2023, time.January, 2, 3, 4, 5, 0, time.UTC)
	w := &shortcuts.Workflow{
		Shortcut: shortcuts.Shortcut{
			Name:        "Deploy 🚀",
			WorkflowID:  "1E7B0A6E-0000-4000-8000-000000000000",
			ActionCount: 3,
			Created:     created,
			Modified:    created.Add(time.Hour),
			Icon:        shortcuts.Icon{Glyph: 59511, Color: 0xFFD426FF},
		},
		ClientVersion:           "3072.2.2",
		MinimumClientVersion:    "900",
		InputContentItemClasses: []any{"WFStringContentItem"},
		ShowInSearch:            true,
	}

	m := NewMetadata(w)
	if m.Color != "#FFD426" {
		t.Errorf("Color = %q, want #FFD426", m.Color)
	}
	if m.Created != created.Format(time.RFC3339) {
		t.Errorf("Created = %q, want RFC3339", m.Created)
	}
	if len(m.WorkflowTypes) != 3 {
		t.Errorf("WorkflowTypes = %v, want all three surfaces", m.WorkflowTypes)
	}

	path := filepath.Join(t.TempDir(), "meta", "metadata.json")
	if err := WriteMetadata(path, m); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	var round Metadata
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("round trip: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(round, m) {
		t.Errorf("round tripped metadata = %+v, want %+v", round, m)
	}
	if !bytes.HasSuffix(data, []byte("}\n")) {
		t.Errorf("metadata.json does not end with a single newline: %q", data)
	}

	if err := WriteMetadata(path, m); err != nil {
		t.Fatalf("second WriteMetadata: %v", err)
	}
}

func TestNewMetadataRendersZeroDatesAsEmptyStrings(t *testing.T) {
	m := NewMetadata(&shortcuts.Workflow{})
	if m.Created != "" || m.Modified != "" {
		t.Errorf("zero dates = %q/%q, want empty strings", m.Created, m.Modified)
	}
}

func TestCanonicalizeWalksIntoStructFields(t *testing.T) {
	type inner struct {
		Blob []byte    `json:"blob"`
		When time.Time `json:"when"`
	}
	type outer struct {
		Name  string `json:"name"`
		Inner inner  `json:"inner"`
	}

	value := outer{Name: "x", Inner: inner{
		Blob: []byte("hello"),
		When: time.Unix(1700000000, 0).UTC(),
	}}

	data, err := CanonicalJSON(value)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}

	var round map[string]any
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("canonical JSON does not parse: %v\n%s", err, data)
	}
	if round["name"] != "x" {
		t.Errorf("json tags were not honoured: %s", data)
	}
	nested, ok := round["inner"].(map[string]any)
	if !ok {
		t.Fatalf("inner is %T, want a nested object: %s", round["inner"], data)
	}
	if _, present := nested["blob"]; !present {
		t.Errorf("nested []byte field was dropped: %s", data)
	}
	if _, present := nested["when"]; !present {
		t.Errorf("nested time field was dropped: %s", data)
	}
}

func TestCanonicalJSONForMetadataIsUnchangedByStructHandling(t *testing.T) {
	m := Metadata{
		Name: "Encode URL", WorkflowID: "44434252-BEA5-4DA6-BC72-949BD054B2E3",
		ActionCount: 7, Created: "2025-10-23T19:22:25-07:00", Modified: "2025-11-05T13:50:21-07:00",
		Glyph: 61588, Color: "#FE9949", ClientVersion: "2038.0.1.10",
		MinimumClientVersion: "1113", WorkflowTypes: []string{"WFWorkflowTypeShowInSearch"},
	}
	data, err := CanonicalJSON(m)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	for _, want := range []string{
		`"name": "Encode URL"`,
		`"action_count": 7`,
		`"color": "#FE9949"`,
		`"workflow_types": [`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("metadata JSON is missing %s:\n%s", want, data)
		}
	}
}
