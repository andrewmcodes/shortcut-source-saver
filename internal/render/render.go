package render

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"
	"unicode"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/catalog"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

//go:embed templates assets
var files embed.FS

const (
	timestampLayout  = "2006-01-02 15:04 MST"
	StylesheetPath   = "assets/style.css"
	IndexPath        = "index.html"
	ShortcutsDirname = "shortcuts"
	ShortcutFilename = "shortcut.shortcut"
	ReviewFilename   = "review.html"
)

var (
	pages    = template.Must(template.ParseFS(files, "templates/*.html.tmpl"))
	markdown = texttemplate.Must(texttemplate.ParseFS(files, "templates/*.md.tmpl"))
	styles   = mustReadStyles()
)

func mustReadStyles() string {
	var b strings.Builder
	for _, name := range []string{"assets/shortcut-source-tool.css", "assets/site.css"} {
		data, err := files.ReadFile(name)
		if err != nil {
			panic("render: missing embedded stylesheet " + name)
		}
		b.Write(data)
		b.WriteString("\n")
	}
	return b.String()
}

func Stylesheet() []byte { return []byte(styles) }

type Options struct {
	ExpandAll    bool
	SiteURL      string
	ShortcutFile string
}

type Entry struct {
	Workflow *shortcuts.Workflow
	Slug     string
}

type SiteOptions struct {
	Title     string
	BaseURL   string
	ExpandAll bool
}

type page struct {
	Title      string
	InlineCSS  template.CSS
	Stylesheet string
	Home       string
	Header     pageHeader
	Items      []*item
}

type pageHeader struct {
	Name        string
	ActionCount int
	MinimumOS   string
	Color       template.CSS
	ColorText   string
	Rows        []metaRow
	Corrupted   bool
	CorruptedAt int
}

type metaRow struct {
	Label string
	Text  string
	Nodes []node
}

func WebReview(w *shortcuts.Workflow) ([]byte, error) {
	return WebReviewWithOptions(w, Options{})
}

func WebReviewWithOptions(w *shortcuts.Workflow, opts Options) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("render: no workflow")
	}
	rendered := buildPage(w, opts.ExpandAll)
	rendered.InlineCSS = template.CSS(styles)
	return execute(pages, "review", rendered)
}

func buildPage(w *shortcuts.Workflow, expandAll bool) *page {
	reviewer := newReviewer(w.Actions, expandAll)
	built := reviewer.build(w.Actions)
	return &page{
		Title:  w.Name,
		Header: headerFor(w, built, reviewer),
		Items:  built.Items,
	}
}

func headerFor(w *shortcuts.Workflow, built *review, r *reviewer) pageHeader {
	color := archive.HexColor(w.Icon.Color)
	header := pageHeader{
		Name:        w.Name,
		ActionCount: len(w.Actions),
		MinimumOS:   minimumOS(w),
		Color:       template.CSS(color),
		ColorText:   color,
		CorruptedAt: -1,
	}

	header.Rows = []metaRow{
		{Label: "Identifier", Text: w.WorkflowID},
		{Label: "Created", Text: formatTime(w.Created)},
		{Label: "Modified", Text: formatTime(w.Modified)},
		{Label: "Icon", Text: fmt.Sprintf("%s, glyph %d", color, w.Icon.Glyph)},
		{Label: "Client version", Text: w.ClientVersion},
		{Label: "Minimum client version", Text: w.MinimumClientVersion},
		{Label: "Workflow types", Text: strings.Join(archive.WorkflowTypes(w), ", ")},
	}
	for _, row := range workflowDetails(w) {
		header.Rows = append(header.Rows, metaRow{Label: row.Label, Text: row.Value})
	}
	for _, question := range w.ImportQuestions {
		header.Rows = append(header.Rows, metaRow{Label: "Import question", Nodes: r.importQuestion(question)})
	}
	if built.Corrupted {
		header.Corrupted = true
		header.CorruptedAt = built.CorruptedAt
		header.Rows = append(header.Rows, metaRow{
			Label: "Control flow",
			Text:  fmt.Sprintf("action %d does not match the block it closes", built.CorruptedAt),
		})
	}
	if built.Unclosed {
		header.Corrupted = true
		if header.CorruptedAt < 0 {
			header.CorruptedAt = built.UnclosedAt
		}
		header.Rows = append(header.Rows, metaRow{
			Label: "Control flow",
			Text:  fmt.Sprintf("the block opened by action %d is never closed", built.UnclosedAt),
		})
	}
	if built.MalformedAt >= 0 {
		header.Corrupted = true
		if header.CorruptedAt < 0 {
			header.CorruptedAt = built.MalformedAt
		}
		header.Rows = append(header.Rows, metaRow{
			Label: "Malformed value",
			Text:  fmt.Sprintf("action %d: %s", built.MalformedAt, built.Malformed),
		})
	}
	return header
}

type detailRow struct {
	Label string
	Value string
}

func workflowDetails(w *shortcuts.Workflow) []detailRow {
	var rows []detailRow
	if accepts := contentClasses(w.InputContentItemClasses); accepts != "" {
		rows = append(rows, detailRow{"Accepts", accepts})
	}
	if output := contentClasses(w.OutputContentItemClasses); output != "" {
		rows = append(rows, detailRow{"Output", output})
	}
	if w.HasOutputFallback {
		rows = append(rows, detailRow{"Output fallback", "yes"})
	}
	if w.HasShortcutInputVariables {
		rows = append(rows, detailRow{"Shortcut input variables", "yes"})
	}
	return rows
}

func contentClasses(classes []any) string {
	names := make([]string, 0, len(classes))
	for _, raw := range classes {
		if class, ok := raw.(string); ok && class != "" {
			names = append(names, splitWords(contentClassName(class)))
		}
	}
	return strings.Join(names, ", ")
}

func contentClassName(class string) string {
	if i := strings.LastIndex(class, "_"); i >= 0 && i < len(class)-1 {
		return strings.TrimSuffix(class[i+1:], "Entity")
	}
	return coercionName(class)
}

func splitWords(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) && !(i == 1 && unicode.IsLower(runes[0])) {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (r *reviewer) importQuestion(raw any) []node {
	question, ok := raw.(map[string]any)
	if !ok {
		return r.value(raw)
	}
	var out []node
	if index, ok := intAt(question, "ActionIndex"); ok {
		out = append(out, magic(index, text("action "+strconv.Itoa(index))), text(" · "))
	}
	if key := stringOr(question, "ParameterKey"); key != "" {
		out = append(out, visibleText(strings.TrimPrefix(key, "WF"))...)
		out = append(out, text(": "))
	}
	out = append(out, visibleText(stringOr(question, "Text"))...)
	if value, ok := question["DefaultValue"]; ok && value != "" {
		out = append(out, text(" (default "), boxOf(r.value(value)), text(")"))
	}
	return out
}

func minimumOS(w *shortcuts.Workflow) string {
	version, err := strconv.Atoi(strings.TrimSpace(w.MinimumClientVersion))
	if err != nil {
		return ""
	}
	os, ok := catalog.MinimumOSVersion(version)
	if !ok {
		return ""
	}
	return os
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(timestampLayout)
}

type readmeData struct {
	Name                 string
	ActionCount          int
	Created              string
	Modified             string
	Color                string
	Glyph                int64
	MinimumOS            string
	ClientVersion        string
	MinimumClientVersion string
	WorkflowTypes        string
	ShortcutFile         string
	ShortcutHref         string
	ReviewLink           string
	Corrupted            bool
	CorruptedAt          int
	Malformed            string
	MalformedAt          int
	Details              []detailRow
	ImportQuestions      []string
	Outline              []readmeOutline
}

type readmeOutline struct {
	Indent    string
	Index     int
	Summary   string
	Corrupted bool
	Malformed bool
}

func ShortcutREADME(w *shortcuts.Workflow) ([]byte, error) {
	return ShortcutREADMEWithOptions(w, Options{})
}

func ShortcutREADMEWithOptions(w *shortcuts.Workflow, opts Options) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("render: no workflow")
	}
	reviewer := newReviewer(w.Actions, opts.ExpandAll)
	built := reviewer.build(w.Actions)

	data := readmeData{
		Name:                 w.Name,
		ActionCount:          len(w.Actions),
		Created:              formatTime(w.Created),
		Modified:             formatTime(w.Modified),
		Color:                archive.HexColor(w.Icon.Color),
		Glyph:                w.Icon.Glyph,
		MinimumOS:            minimumOS(w),
		ClientVersion:        w.ClientVersion,
		MinimumClientVersion: w.MinimumClientVersion,
		WorkflowTypes:        strings.Join(archive.WorkflowTypes(w), ", "),
		ShortcutFile:         shortcutFile(opts.ShortcutFile),
		ShortcutHref:         (&url.URL{Path: shortcutFile(opts.ShortcutFile)}).String(),
		ReviewLink:           reviewLink(w, opts.SiteURL),
		CorruptedAt:          -1,
		Malformed:            built.Malformed,
		MalformedAt:          built.MalformedAt,
		Details:              workflowDetails(w),
	}
	for _, question := range w.ImportQuestions {
		data.ImportQuestions = append(data.ImportQuestions, summarize(reviewer.importQuestion(question)))
	}
	switch {
	case built.Corrupted:
		data.Corrupted = true
		data.CorruptedAt = built.CorruptedAt
	case built.Unclosed:
		data.Corrupted = true
		data.CorruptedAt = built.UnclosedAt
	}
	for _, entry := range built.Outline {
		data.Outline = append(data.Outline, readmeOutline{
			Indent:    strings.Repeat("  ", entry.Depth),
			Index:     entry.Index,
			Summary:   entry.Summary,
			Corrupted: entry.Corrupted,
			Malformed: entry.Malformed,
		})
	}

	return execute(markdown, "readme", data)
}

func shortcutFile(name string) string {
	if name == "" {
		return ShortcutFilename
	}
	return name
}

func reviewLink(w *shortcuts.Workflow, siteURL string) string {
	if siteURL == "" {
		return ReviewFilename
	}
	return strings.TrimSuffix(siteURL, "/") + "/" + path.Join(ShortcutsDirname, archive.Slug(w.Name)+".html")
}

type indexPage struct {
	Title      string
	Stylesheet string
	Rows       []indexRow
}

type indexRow struct {
	Name         string
	Href         string
	ActionCount  int
	Modified     string
	ModifiedSort string
	Color        template.CSS
	ColorText    string
}

func Site(entries []Entry, opts SiteOptions) (map[string][]byte, error) {
	out := map[string][]byte{StylesheetPath: Stylesheet()}
	index := indexPage{Title: siteTitle(opts), Stylesheet: prefix(opts.BaseURL, 0) + StylesheetPath}

	slugs := map[string]bool{}
	rows := make([]indexRow, 0, len(entries))
	for _, entry := range entries {
		if entry.Workflow == nil {
			return nil, fmt.Errorf("render: site entry has no workflow")
		}
		slug := uniqueSlug(entry, slugs)

		rendered := buildPage(entry.Workflow, opts.ExpandAll)
		rendered.Stylesheet = prefix(opts.BaseURL, 1) + StylesheetPath
		rendered.Home = prefix(opts.BaseURL, 1) + IndexPath
		body, err := execute(pages, "review", rendered)
		if err != nil {
			return nil, err
		}
		out[path.Join(ShortcutsDirname, slug+".html")] = body

		color := archive.HexColor(entry.Workflow.Icon.Color)
		rows = append(rows, indexRow{
			Name:         entry.Workflow.Name,
			Href:         prefix(opts.BaseURL, 0) + path.Join(ShortcutsDirname, slug+".html"),
			ActionCount:  len(entry.Workflow.Actions),
			Modified:     formatTime(entry.Workflow.Modified),
			ModifiedSort: sortableTime(entry.Workflow.Modified),
			Color:        template.CSS(color),
			ColorText:    color,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left, right := strings.ToLower(rows[i].Name), strings.ToLower(rows[j].Name)
		if left != right {
			return left < right
		}
		return rows[i].Href < rows[j].Href
	})
	index.Rows = rows

	body, err := execute(pages, "index", index)
	if err != nil {
		return nil, err
	}
	out[IndexPath] = body
	return out, nil
}

func siteTitle(opts SiteOptions) string {
	if opts.Title == "" {
		return "Shortcuts"
	}
	return opts.Title
}

func uniqueSlug(entry Entry, taken map[string]bool) string {
	slug := entry.Slug
	if slug == "" {
		slug = archive.Slug(entry.Workflow.Name)
	}
	candidate := slug
	for n := 2; taken[candidate]; n++ {
		candidate = slug + "-" + strconv.Itoa(n)
	}
	taken[candidate] = true
	return candidate
}

func prefix(baseURL string, depth int) string {
	if baseURL != "" {
		return strings.TrimSuffix(baseURL, "/") + "/"
	}
	return strings.Repeat("../", depth)
}

func sortableTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

type executor interface {
	ExecuteTemplate(wr io.Writer, name string, data any) error
}

func execute(t executor, name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("render: %s: %w", name, err)
	}
	return buf.Bytes(), nil
}
