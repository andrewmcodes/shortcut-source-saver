package render

import (
	"html/template"
	"sort"
	"strconv"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/catalog"
)

const (
	runShortcutIdentifier = "is.workflow.actions.runworkflow"
	runWorkflowParameter  = "WFWorkflow"
	runNameParameter      = "WFWorkflowName"
)

type callStatus int

const (
	callLinked callStatus = iota
	callByName
	callSelf
	callMissing
	callDynamic
)

type runTarget struct {
	ID      string
	Name    string
	Self    bool
	Dynamic bool
}

type call struct {
	Action    int
	Status    callStatus
	Target    int
	Name      string
	ID        string
	Ambiguous bool
}

func runShortcutTarget(params map[string]any) runTarget {
	var t runTarget
	switch descriptor := params[runWorkflowParameter].(type) {
	case map[string]any:
		if isVariableValue(descriptor) {
			t.Dynamic = true
			return t
		}
		t.ID = strings.TrimSpace(stringOr(descriptor, "workflowIdentifier"))
		t.Name = stringOr(descriptor, "workflowName")
		t.Self = isTrue(descriptor["isSelf"])
	case string:
		t.Name = descriptor
	}
	switch name := params[runNameParameter].(type) {
	case string:
		if t.Name == "" {
			t.Name = name
		}
	case map[string]any:
		if t.ID == "" && t.Name == "" && !t.Self {
			t.Dynamic = true
		}
	}
	return t
}

func isVariableValue(m map[string]any) bool {
	for _, key := range []string{"workflowIdentifier", "workflowName", "isSelf"} {
		if _, ok := m[key]; ok {
			return false
		}
	}
	_, value := m["Value"]
	_, serialized := m["WFSerializationType"]
	_, typed := m["Type"]
	return value || serialized || typed
}

func isTrue(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	n, ok := toInt(v)
	return ok && n == 1
}

type libraryIndex struct {
	byID   map[string][]int
	byName map[string][]int
}

func indexLibrary(entries []Entry) libraryIndex {
	idx := libraryIndex{byID: map[string][]int{}, byName: map[string][]int{}}
	for i, entry := range entries {
		if id := strings.ToUpper(strings.TrimSpace(entry.Workflow.WorkflowID)); id != "" {
			idx.byID[id] = append(idx.byID[id], i)
		}
		idx.byName[entry.Workflow.Name] = append(idx.byName[entry.Workflow.Name], i)
	}
	return idx
}

func (idx libraryIndex) resolve(caller int, entries []Entry, action int, t runTarget) call {
	c := call{Action: action, Target: -1, Name: t.Name, ID: t.ID}
	switch {
	case t.Dynamic:
		c.Status = callDynamic
		return c
	case t.Self:
		c.Status, c.Target = callSelf, caller
		return c
	}
	if t.ID != "" {
		matches := idx.byID[strings.ToUpper(t.ID)]
		if len(matches) > 1 {
			matches = withName(matches, entries, t.Name)
		}
		if len(matches) == 1 {
			c.Status, c.Target = callLinked, matches[0]
			if c.Target == caller {
				c.Status = callSelf
			}
			return c
		}
	}
	if t.Name != "" {
		matches := idx.byName[t.Name]
		if len(matches) == 1 {
			c.Status, c.Target = callByName, matches[0]
			if c.Target == caller {
				c.Status = callSelf
			}
			return c
		}
		c.Ambiguous = len(matches) > 1
	}
	c.Status = callMissing
	return c
}

func withName(matches []int, entries []Entry, name string) []int {
	var out []int
	for _, i := range matches {
		if entries[i].Workflow.Name == name {
			out = append(out, i)
		}
	}
	return out
}

func resolveCalls(entries []Entry) [][]call {
	idx := indexLibrary(entries)
	out := make([][]call, len(entries))
	for i, entry := range entries {
		for action, raw := range entry.Workflow.Actions {
			if actionIdentifier(raw) != runShortcutIdentifier {
				continue
			}
			target := runShortcutTarget(actionParameters(raw))
			out[i] = append(out[i], idx.resolve(i, entries, action, target))
		}
	}
	return out
}

func (c call) resolved() bool { return c.Status == callLinked || c.Status == callByName }

type relation struct {
	Name     string
	Href     string
	Color    template.CSS
	DarkIcon bool
	Note     string
	Kind     string
	Detail   string
	Actions  []int
}

type runLink struct {
	call
	Href       string
	TargetName string
}

type pageDeps struct {
	links    map[int]runLink
	Runs     []relation
	CalledBy []relation
	MapHref  string
}

type siteGraph struct {
	entries []Entry
	slugs   []string
	calls   [][]call
	callers [][]int
}

func newSiteGraph(entries []Entry, slugs []string) *siteGraph {
	g := &siteGraph{entries: entries, slugs: slugs, calls: resolveCalls(entries), callers: make([][]int, len(entries))}
	for i, calls := range g.calls {
		seen := map[int]bool{}
		for _, c := range calls {
			if c.resolved() && !seen[c.Target] {
				seen[c.Target] = true
				g.callers[c.Target] = append(g.callers[c.Target], i)
			}
		}
	}
	return g
}

func (g *siteGraph) connected(i int) bool {
	return len(g.calls[i]) > 0 || len(g.callers[i]) > 0
}

func (g *siteGraph) outgoing(i int) int {
	keys := map[string]bool{}
	for _, c := range g.calls[i] {
		keys[relationKey(c)] = true
	}
	return len(keys)
}

func (g *siteGraph) missing(i int) int {
	keys := map[string]bool{}
	for _, c := range g.calls[i] {
		if c.Status == callMissing {
			keys[relationKey(c)] = true
		}
	}
	return len(keys)
}

func relationKey(c call) string {
	switch c.Status {
	case callLinked, callByName:
		return "shortcut:" + strconv.Itoa(c.Target)
	case callSelf:
		return "self"
	case callDynamic:
		return "dynamic"
	default:
		return "missing:" + strings.ToUpper(c.ID) + "\x00" + c.Name
	}
}

func (g *siteGraph) pageDeps(i int, href func(slug string) string, mapHref string) *pageDeps {
	deps := &pageDeps{links: map[int]runLink{}, MapHref: mapHref}
	if g.connected(i) {
		deps.MapHref = mapHref + "#" + mapAnchor(g.slugs[i])
	}
	groups := map[string]*relation{}
	var order []string
	for _, c := range g.calls[i] {
		link := runLink{call: c}
		if c.Target >= 0 {
			link.Href = href(g.slugs[c.Target])
			link.TargetName = g.entries[c.Target].Workflow.Name
		}
		deps.links[c.Action] = link
		key := relationKey(c)
		group, ok := groups[key]
		if !ok {
			group = g.relationFor(c, href)
			groups[key] = group
			order = append(order, key)
		}
		group.Actions = append(group.Actions, c.Action)
	}
	for _, key := range order {
		deps.Runs = append(deps.Runs, *groups[key])
	}
	for _, caller := range g.callers[i] {
		if caller == i {
			continue
		}
		deps.CalledBy = append(deps.CalledBy, g.shortcutRelation(caller, href, "", ""))
	}
	sortRelations(deps.CalledBy)
	return deps
}

func (g *siteGraph) relationFor(c call, href func(slug string) string) *relation {
	switch c.Status {
	case callLinked:
		note := ""
		if name := g.entries[c.Target].Workflow.Name; c.Name != "" && c.Name != name {
			note = "Recorded as " + c.Name
		}
		r := g.shortcutRelation(c.Target, href, "Linked by identifier", note)
		return &r
	case callByName:
		r := g.shortcutRelation(c.Target, href, "Matched by name", "")
		if c.ID != "" {
			r.Detail = "Identifier " + c.ID + " is not in this backup"
		}
		return &r
	case callSelf:
		return &relation{Name: g.entries[c.Target].Workflow.Name, Kind: "self", Note: "This shortcut runs itself"}
	case callDynamic:
		return &relation{Name: "Chosen at run time", Kind: "dynamic", Note: "The shortcut to run comes from a variable"}
	default:
		name := c.Name
		if name == "" {
			name = "No shortcut chosen"
		}
		r := &relation{Name: name, Kind: "missing", Note: "Not in this backup"}
		if c.Ambiguous {
			r.Note = "Several shortcuts in this backup have this name"
		}
		if c.ID != "" {
			r.Detail = c.ID
		}
		return r
	}
}

func (g *siteGraph) shortcutRelation(i int, href func(slug string) string, note, detail string) relation {
	color := archive.HexColor(g.entries[i].Workflow.Icon.Color)
	return relation{
		Name:     g.entries[i].Workflow.Name,
		Href:     href(g.slugs[i]),
		Color:    template.CSS(color),
		DarkIcon: isDarkColor(color),
		Note:     note,
		Detail:   detail,
		Kind:     "shortcut",
	}
}

func sortRelations(rs []relation) {
	sort.SliceStable(rs, func(a, b int) bool {
		left, right := strings.ToLower(rs[a].Name), strings.ToLower(rs[b].Name)
		if left != right {
			return left < right
		}
		return rs[a].Href < rs[b].Href
	})
}

func mapAnchor(slug string) string { return "map-" + slug }

func (r *reviewer) applyRunTarget(act catalog.Action, params map[string]any, subs map[catalog.Slot][]node, consumed map[string]bool) []paramRow {
	link, ok := r.deps.links[r.current]
	if !ok || link.Status == callDynamic {
		return nil
	}
	if _, descriptor := params[runWorkflowParameter].(map[string]any); descriptor {
		consumed[runWorkflowParameter] = true
	}
	name := link.Name
	if name == "" {
		name = link.TargetName
	}
	var title []node
	var detail []node
	switch link.Status {
	case callLinked, callByName:
		title = []node{pageLink(link.Href, visibleText(name)...)}
		if link.Status == callLinked {
			detail = textNodes("Linked by identifier " + link.ID)
		} else {
			detail = textNodes("Matched by name")
			if link.ID != "" {
				detail = textNodes("Matched by name; identifier " + link.ID + " is not in this backup")
			}
		}
		if link.TargetName != name {
			detail = append(detail, text(" · now named "))
			detail = append(detail, visibleText(link.TargetName)...)
		}
	case callSelf:
		title = visibleText(name)
		detail = textNodes("This shortcut, run recursively")
	default:
		if name == "" {
			name = "no shortcut chosen"
		}
		title = []node{wrap(kindMissing, visibleText(name)...)}
		switch {
		case link.Ambiguous:
			detail = textNodes("Missing reference: several shortcuts in this backup are named this")
		case link.ID != "":
			detail = textNodes("Missing reference: identifier " + link.ID + " is not in this backup")
		default:
			detail = textNodes("Missing reference: no shortcut in this backup has this name")
		}
	}
	if slot, ok := act.SlotFor(runNameParameter); ok {
		consumed[runNameParameter] = true
		subs[slot] = []node{boxOf(title)}
	}
	return []paramRow{{Label: "Shortcut", Value: detail}}
}
