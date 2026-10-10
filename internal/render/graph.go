package render

import (
	"html/template"
	"sort"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
)

type focusLink struct {
	Key      string
	Name     string
	Href     string
	Color    template.CSS
	DarkIcon bool
	Kind     string
	Note     string
}

type focusRow struct {
	Rank        int
	Key         string
	Anchor      string
	Name        string
	Href        string
	Color       template.CSS
	DarkIcon    bool
	Actions     int
	Callers     []focusLink
	Runs        []focusLink
	Self        bool
	CallerCount int
	RunCount    int
	Missing     int
}

func (r focusRow) total() int { return r.CallerCount + r.RunCount }

type mapPage struct {
	Title      string
	Stylesheet string
	Chrome     chrome
	Home       string
	Connected  int
	Standalone int
	Missing    int
	Dynamic    int
	Rows       []focusRow
}

func (g *siteGraph) mapPage(href func(slug string) string) mapPage {
	page := mapPage{}
	for i := range g.entries {
		page.Missing += g.missing(i)
		for _, c := range g.calls[i] {
			if c.Status == callDynamic {
				page.Dynamic++
			}
		}
		if !g.connected(i) {
			page.Standalone++
			continue
		}
		page.Rows = append(page.Rows, g.focusRow(i, href))
	}
	page.Connected = len(page.Rows)
	sort.SliceStable(page.Rows, func(a, b int) bool {
		left, right := page.Rows[a], page.Rows[b]
		if left.total() != right.total() {
			return left.total() > right.total()
		}
		return lessName(left.Name, left.Key, right.Name, right.Key)
	})
	for i := range page.Rows {
		page.Rows[i].Rank = i + 1
	}
	return page
}

func (g *siteGraph) focusRow(i int, href func(slug string) string) focusRow {
	w := g.entries[i].Workflow
	color := archive.HexColor(w.Icon.Color)
	row := focusRow{
		Key:      g.slugs[i],
		Anchor:   mapAnchor(g.slugs[i]),
		Name:     w.Name,
		Href:     href(g.slugs[i]),
		Color:    template.CSS(color),
		DarkIcon: isDarkColor(color),
		Actions:  len(w.Actions),
		Missing:  g.missing(i),
	}
	seen := map[string]bool{}
	for _, c := range g.calls[i] {
		key := relationKey(c)
		if seen[key] {
			continue
		}
		seen[key] = true
		switch c.Status {
		case callSelf:
			row.Self = true
			continue
		case callLinked, callByName:
			row.Runs = append(row.Runs, g.focusShortcut(c.Target, href))
		case callDynamic:
			row.Runs = append(row.Runs, focusLink{Name: "Chosen at run time", Kind: "dynamic", Note: "From a variable"})
		default:
			name := c.Name
			if name == "" {
				name = "No shortcut chosen"
			}
			note := "Not in this backup"
			if c.Ambiguous {
				note = "Several shortcuts have this name"
			}
			row.Runs = append(row.Runs, focusLink{Name: name, Kind: "missing", Note: note})
		}
	}
	for _, caller := range g.callers[i] {
		if caller != i {
			row.Callers = append(row.Callers, g.focusShortcut(caller, href))
		}
	}
	sort.SliceStable(row.Callers, func(a, b int) bool {
		return lessName(row.Callers[a].Name, row.Callers[a].Key, row.Callers[b].Name, row.Callers[b].Key)
	})
	row.CallerCount, row.RunCount = len(row.Callers), len(row.Runs)
	return row
}

func (g *siteGraph) focusShortcut(i int, href func(slug string) string) focusLink {
	color := archive.HexColor(g.entries[i].Workflow.Icon.Color)
	return focusLink{
		Key:      g.slugs[i],
		Name:     g.entries[i].Workflow.Name,
		Href:     href(g.slugs[i]),
		Color:    template.CSS(color),
		DarkIcon: isDarkColor(color),
		Kind:     "shortcut",
	}
}

func lessName(leftName, leftKey, rightName, rightKey string) bool {
	left, right := strings.ToLower(leftName), strings.ToLower(rightName)
	if left != right {
		return left < right
	}
	return leftKey < rightKey
}
