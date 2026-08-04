package render

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

func TestTmpRealWorkflow(t *testing.T) {
	raw, err := os.ReadFile("../../.ai/shortcut_source_tool.json")
	if err != nil {
		t.Skip(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	actions, _ := decoded["WFWorkflowActions"].([]any)
	w := &shortcuts.Workflow{
		Shortcut:             shortcuts.Shortcut{Name: "Shortcut Source Tool", ActionCount: len(actions)},
		Actions:              actions,
		ClientVersion:        "3216",
		MinimumClientVersion: "3010",
	}
	page, err := WebReview(w)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actions=%d page=%d bytes", len(actions), len(page))
	assertTagsBalanced(t, page)
	if err := os.WriteFile("/tmp/real-review.html", page, 0o644); err != nil {
		t.Fatal(err)
	}
	readme, err := ShortcutREADME(w)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile("/tmp/real-readme.md", readme, 0o644)
	if strings.Contains(string(page), "?type?") {
		t.Log("note: unhandled Type values present")
	}
}
