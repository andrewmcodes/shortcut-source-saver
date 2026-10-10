package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

const (
	helperID  = "AAAAAAAA-0000-0000-0000-000000000001"
	utilityID = "BBBBBBBB-0000-0000-0000-000000000002"
	callerID  = "CCCCCCCC-0000-0000-0000-000000000003"
)

func withID(w *shortcuts.Workflow, id string) *shortcuts.Workflow {
	w.WorkflowID = id
	return w
}

func runShortcut(id, name string, self bool) map[string]any {
	return action(runShortcutIdentifier, map[string]any{
		"WFWorkflow":     map[string]any{"workflowIdentifier": id, "workflowName": name, "isSelf": self},
		"WFWorkflowName": name,
	})
}

func dependencyEntries() []Entry {
	helper := withID(workflow("Helper",
		runShortcut(utilityID, "Utility", false),
	), helperID)
	utility := withID(workflow("Utility",
		action("is.workflow.actions.gettext", map[string]any{"WFTextActionText": "done"}),
	), utilityID)
	caller := withID(workflow("Caller",
		runShortcut(helperID, "Helper", false),
		runShortcut("DEADBEEF-0000-0000-0000-000000000000", "Utility", false),
		runShortcut("11111111-0000-0000-0000-000000000000", "Caller", true),
		runShortcut("99999999-0000-0000-0000-000000000000", "Ghost", false),
		action(runShortcutIdentifier, map[string]any{
			"WFWorkflow":     attachment(actionOutput("A", "Chosen Item")),
			"WFWorkflowName": attachment(actionOutput("A", "Chosen Item")),
		}),
		action(runShortcutIdentifier, map[string]any{"WFWorkflowName": "Helper"}),
		runShortcut(helperID, "Old Helper Name", false),
	), callerID)
	return []Entry{{Workflow: caller}, {Workflow: helper}, {Workflow: utility}}
}

func TestRunShortcutResolution(t *testing.T) {
	entries := dependencyEntries()
	calls := resolveCalls(entries)[0]
	want := []struct {
		status callStatus
		target int
	}{
		{callLinked, 1},
		{callByName, 2},
		{callSelf, 0},
		{callMissing, -1},
		{callDynamic, -1},
		{callByName, 1},
		{callLinked, 1},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i, w := range want {
		if calls[i].Action != i || calls[i].Status != w.status || calls[i].Target != w.target {
			t.Errorf("call %d = %+v, want status %d target %d", i, calls[i], w.status, w.target)
		}
	}
}

func TestRunShortcutByIdentifierMatchingTheCallerIsSelf(t *testing.T) {
	loop := withID(workflow("Loop", runShortcut(callerID, "Loop", false)), callerID)
	calls := resolveCalls([]Entry{{Workflow: loop}})[0]
	if len(calls) != 1 || calls[0].Status != callSelf {
		t.Fatalf("calls = %+v, want one self call", calls)
	}
}

func TestRunShortcutWithAnAmbiguousNameIsMissing(t *testing.T) {
	first := withID(workflow("Twin", action("is.workflow.actions.nothing", nil)), helperID)
	second := withID(workflow("Twin", action("is.workflow.actions.nothing", nil)), utilityID)
	caller := withID(workflow("Caller", action(runShortcutIdentifier, map[string]any{"WFWorkflowName": "Twin"})), callerID)
	calls := resolveCalls([]Entry{{Workflow: caller}, {Workflow: first}, {Workflow: second}})[0]
	if len(calls) != 1 || calls[0].Status != callMissing || !calls[0].Ambiguous {
		t.Fatalf("calls = %+v, want one ambiguous missing call", calls)
	}
}

func TestSiteShowsRunsAndCalledBy(t *testing.T) {
	site, err := Site(dependencyEntries(), SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	helper := string(site["shortcuts/helper.html"])
	for _, want := range []string{
		`<h3 id="callers-heading">Called by <span>1</span></h3>`,
		`<a href="../shortcuts/caller.html">`,
		`<h3 id="runs-heading">Runs <span>1</span></h3>`,
		`<a href="../shortcuts/utility.html">`,
		`href="../dependencies.html#map-helper"`,
	} {
		if !strings.Contains(helper, want) {
			t.Errorf("helper page is missing %s", want)
		}
	}
	utility := string(site["shortcuts/utility.html"])
	if !strings.Contains(utility, `Called by <span>2</span>`) {
		t.Error("utility should be called by both the caller and the helper")
	}
	if !strings.Contains(utility, `No Run Shortcut actions.`) {
		t.Error("utility runs nothing")
	}

	caller := string(site["shortcuts/caller.html"])
	for _, want := range []string{
		`<span class="magic page-link" data-href="../shortcuts/helper.html" role="link" tabindex="0">Helper</span>`,
		`<span class="missing-ref">Ghost</span>`,
		`Missing reference: identifier 99999999-0000-0000-0000-000000000000 is not in this backup`,
		`Matched by name; identifier DEADBEEF-0000-0000-0000-000000000000 is not in this backup`,
		`This shortcut, run recursively`,
		`Nothing in this backup runs it.`,
		`<p class="related-steps">Actions <span class="magic" data-target="0" role="link" tabindex="0">0</span> <span class="magic" data-target="5" role="link" tabindex="0">5</span> <span class="magic" data-target="6" role="link" tabindex="0">6</span></p>`,
	} {
		if !strings.Contains(caller, want) {
			t.Errorf("caller page is missing %s", want)
		}
	}

	index := string(site[IndexPath])
	for _, want := range []string{
		`href="shortcuts/caller.html" data-actions="7" data-modified="2025-06-14T17:05:00Z" data-connected data-missing>`,
		`1 missing reference</span>`,
		`Connected (3)`,
		`Missing references (1)`,
		`href="dependencies.html"`,
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index is missing %s", want)
		}
	}
	assertTagsBalanced(t, site[MapPath])
	assertTagsBalanced(t, site["shortcuts/caller.html"])
	assertGolden(t, "site-map.html", site[MapPath])
	assertGolden(t, "site-calls-page.html", site["shortcuts/caller.html"])
}

func TestStandaloneShortcutsAreMarked(t *testing.T) {
	site, err := Site([]Entry{{Workflow: fixtureNamed(t, "linear")}}, SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	if !strings.Contains(string(site[IndexPath]), `<span class="card-links">Standalone</span>`) {
		t.Error("a shortcut with no calls or callers should read Standalone")
	}
	if !strings.Contains(string(site[MapPath]), "No shortcut in this backup runs another.") {
		t.Error("the map should say when nothing is connected")
	}
}

func TestUnrelatedShortcutDoesNotRewritePages(t *testing.T) {
	before, err := Site(dependencyEntries(), SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	unrelated := withID(workflow("Unrelated", action("is.workflow.actions.nothing", nil)), "EEEEEEEE-0000-0000-0000-000000000005")
	after, err := Site(append(dependencyEntries(), Entry{Workflow: unrelated}), SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	for name, body := range before {
		if strings.HasPrefix(name, ShortcutsDirname+"/") && !bytes.Equal(body, after[name]) {
			t.Errorf("%s changed when an unrelated shortcut was added", name)
		}
	}
}

func TestSiteIgnoresEntryOrder(t *testing.T) {
	entries := dependencyEntries()
	first, err := Site(entries, SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	reversed := []Entry{entries[2], entries[1], entries[0]}
	second, err := Site(reversed, SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	for name, body := range first {
		if !bytes.Equal(body, second[name]) {
			t.Errorf("%s depends on the order of the entries", name)
		}
	}
}

func mapRows(t *testing.T, page string) map[string]string {
	t.Helper()
	rows := map[string]string{}
	var order []string
	for _, chunk := range strings.Split(page, `<li class="rank-row" id="map-`)[1:] {
		key := chunk[:strings.Index(chunk, `"`)]
		end := strings.Index(chunk, "</details>")
		if end < 0 {
			t.Fatalf("row %s has no details", key)
		}
		rows[key] = chunk[:end]
		order = append(order, key)
	}
	rows[""] = strings.Join(order, " ")
	return rows
}

func TestMapRanksByConnections(t *testing.T) {
	unrelated := withID(workflow("Unrelated", action("is.workflow.actions.nothing", nil)), "EEEEEEEE-0000-0000-0000-000000000005")
	site, err := Site(append(dependencyEntries(), Entry{Workflow: unrelated}), SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	page := string(site[MapPath])
	rows := mapRows(t, page)
	if got, want := rows[""], "caller helper utility"; got != want {
		t.Errorf("ranking = %q, want %q: most connected first, ties by name, standalone left out", got, want)
	}
	for key, want := range map[string]string{
		"caller":  `<span class="rank-number">1</span>`,
		"helper":  `1 caller · 1 run</small>`,
		"utility": `2 callers · 0 runs</small>`,
	} {
		if !strings.Contains(rows[key], want) {
			t.Errorf("row %s is missing %s", key, want)
		}
	}
	if !strings.Contains(rows["caller"], `0 callers · 4 runs · runs itself</small><small class="rank-warning">`) {
		t.Error("the caller runs four distinct targets besides itself, one of them missing")
	}
	if !strings.Contains(page, "1 standalone shortcut neither runs nor is run by another") {
		t.Error("the map should count the standalone shortcuts it leaves out")
	}
	if !strings.Contains(page, "focusOn(graph.get(keyFromHash()) || nodes[0])") {
		t.Error("without a hash the focus view should open on the top-ranked shortcut")
	}
}

func TestMapListsConnectionsWithoutScript(t *testing.T) {
	site, err := Site(dependencyEntries(), SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	rows := mapRows(t, string(site[MapPath]))
	want := map[string][]string{
		"caller": {
			`<a class="rank-link" href="shortcuts/caller.html">`,
			`<p>Nothing in this backup runs it.</p>`,
			`<li data-relation="runs" data-key="helper"><a href="shortcuts/helper.html">Helper</a></li>`,
			`<li data-relation="runs" data-key="utility"><a href="shortcuts/utility.html">Utility</a></li>`,
			`data-kind="missing" data-note="Not in this backup"><span class="relation-name">Ghost</span>`,
			`data-kind="dynamic" data-note="From a variable"><span class="relation-name">Chosen at run time</span>`,
			`<li data-relation="runs" data-kind="self">Itself</li>`,
		},
		"helper": {
			`<li data-relation="callers" data-key="caller"><a href="shortcuts/caller.html">Caller</a></li>`,
			`<li data-relation="runs" data-key="utility"><a href="shortcuts/utility.html">Utility</a></li>`,
		},
		"utility": {
			`<li data-relation="callers" data-key="caller"><a href="shortcuts/caller.html">Caller</a></li><li data-relation="callers" data-key="helper"><a href="shortcuts/helper.html">Helper</a></li>`,
			`<p>No other shortcut.</p>`,
		},
	}
	for key, fragments := range want {
		for _, fragment := range fragments {
			if !strings.Contains(rows[key], fragment) {
				t.Errorf("row %s is missing %s", key, fragment)
			}
		}
	}
	if n := strings.Count(rows["caller"], `data-relation="runs" data-key="helper"`); n != 1 {
		t.Errorf("caller lists helper %d times, want once however many actions run it", n)
	}
}

func TestMapEscapesShortcutText(t *testing.T) {
	hostile := withID(workflow(`<script>alert(1)</script>`,
		runShortcut(helperID, "Helper", false),
		runShortcut("FFFFFFFF-0000-0000-0000-000000000000", `</script><img src=x>`, false),
	), callerID)
	helper := withID(workflow("Helper", action("is.workflow.actions.nothing", nil)), helperID)
	site, err := Site([]Entry{{Workflow: hostile}, {Workflow: helper}}, SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	for name, body := range site {
		if name == StylesheetPath {
			continue
		}
		if n := bytes.Count(body, []byte("<script")); n != 1 {
			t.Errorf("%s has %d <script tags, want 1", name, n)
		}
		if bytes.Contains(body, []byte("<img")) {
			t.Errorf("%s lets shortcut text add a tag", name)
		}
	}
	page := string(site[MapPath])
	for _, want := range []string{
		`<strong>&lt;script&gt;alert(1)&lt;/script&gt;</strong>`,
		`<span class="relation-name">&lt;/script&gt;&lt;img src=x&gt;</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("map is missing escaped text %s", want)
		}
	}
	assertTagsBalanced(t, site[MapPath])
}
