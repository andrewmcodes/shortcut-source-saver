package render

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"math"
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
	dateLayout       = "2006-01-02"
	StylesheetPath   = "assets/style.css"
	IndexPath        = "index.html"
	MapPath          = "dependencies.html"
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
	for _, name := range []string{"assets/radix-colors.css", "assets/site.css", "assets/actions.css"} {
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
	SiteRoot     string
	Slug         string
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
	Chrome     chrome
	Header     pageHeader
	Items      []*item
}

type chrome struct {
	SiteTitle string
	Home      string
	MapHref   string
	OnMap     bool
}

type pageHeader struct {
	Name        string
	ActionCount int
	MinimumOS   string
	Modified    string
	Color       template.CSS
	ColorText   string
	DarkIcon    bool
	Rows        []metaRow
	Corrupted   bool
	CorruptedAt int
	Deps        *pageDeps
}

type metaRow struct {
	Label   string
	Text    string
	Nodes   []node
	Flagged bool
}

func WebReview(w *shortcuts.Workflow) ([]byte, error) {
	return WebReviewWithOptions(w, Options{})
}

func WebReviewWithOptions(w *shortcuts.Workflow, opts Options) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("render: no workflow")
	}
	rendered := buildPage(w, opts.ExpandAll, nil)
	rendered.InlineCSS = template.CSS(styles)
	rendered.Chrome = chrome{SiteTitle: siteTitle(SiteOptions{})}
	return execute(pages, "review", rendered)
}

func buildPage(w *shortcuts.Workflow, expandAll bool, deps *pageDeps) *page {
	reviewer := newReviewer(w.Actions, expandAll)
	reviewer.deps = deps
	built := reviewer.build(w.Actions)
	header := headerFor(w, built, reviewer)
	header.Deps = deps
	return &page{
		Title:  w.Name,
		Header: header,
		Items:  built.Items,
	}
}

const darkIconLuminance = 0.05

func isDarkColor(hex string) bool {
	value, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil || len(hex) != 7 {
		return false
	}
	linear := func(channel uint64) float64 {
		c := float64(channel) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	luminance := 0.2126*linear(value>>16&0xFF) + 0.7152*linear(value>>8&0xFF) + 0.0722*linear(value&0xFF)
	return luminance < darkIconLuminance
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

func headerFor(w *shortcuts.Workflow, built *review, r *reviewer) pageHeader {
	color := archive.HexColor(w.Icon.Color)
	header := pageHeader{
		Name:        w.Name,
		ActionCount: len(w.Actions),
		MinimumOS:   minimumOS(w),
		Modified:    formatDate(w.Modified),
		Color:       template.CSS(color),
		ColorText:   color,
		DarkIcon:    isDarkColor(color),
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
			Label:   "Control flow",
			Flagged: true,
			Text:    fmt.Sprintf("action %d does not match the block it closes", built.CorruptedAt),
		})
	}
	if built.Unclosed {
		header.Corrupted = true
		if header.CorruptedAt < 0 {
			header.CorruptedAt = built.UnclosedAt
		}
		header.Rows = append(header.Rows, metaRow{
			Label:   "Control flow",
			Flagged: true,
			Text:    fmt.Sprintf("the block opened by action %d is never closed", built.UnclosedAt),
		})
	}
	if built.MalformedAt >= 0 {
		header.Corrupted = true
		if header.CorruptedAt < 0 {
			header.CorruptedAt = built.MalformedAt
		}
		header.Rows = append(header.Rows, metaRow{
			Label:   "Malformed value",
			Flagged: true,
			Text:    fmt.Sprintf("action %d: %s", built.MalformedAt, built.Malformed),
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
		ReviewLink:           reviewLink(w, opts),
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

func reviewLink(w *shortcuts.Workflow, opts Options) string {
	slug := opts.Slug
	if slug == "" {
		slug = archive.Slug(w.Name)
	}
	page := path.Join(ShortcutsDirname, slug+".html")
	switch {
	case isHostedURL(opts.SiteURL):
		return strings.TrimSuffix(opts.SiteURL, "/") + "/" + (&url.URL{Path: page}).String()
	case opts.SiteRoot != "":
		return (&url.URL{Path: path.Join(opts.SiteRoot, page)}).String()
	default:
		return ReviewFilename
	}
}

func isHostedURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme != "" && u.Host != ""
}

type indexPage struct {
	Title        string
	Stylesheet   string
	Chrome       chrome
	MapHref      string
	Count        int
	Actions      int
	Flagged      int
	Connected    int
	Standalone   int
	Missing      int
	LastModified string
	Rows         []indexRow
}

type indexRow struct {
	Name         string
	Href         string
	ActionCount  int
	MinimumOS    string
	ModifiedDate string
	ModifiedSort string
	Color        template.CSS
	ColorText    string
	DarkIcon     bool
	Flagged      bool
	Calls        int
	Callers      int
	Missing      int
}

func Site(entries []Entry, opts SiteOptions) (map[string][]byte, error) {
	out := map[string][]byte{StylesheetPath: Stylesheet()}
	title := siteTitle(opts)
	root := prefix(opts.BaseURL, 0)
	index := indexPage{
		Title:      title,
		Stylesheet: root + StylesheetPath,
		Chrome:     chrome{SiteTitle: title, Home: root + IndexPath, MapHref: root + MapPath},
		MapHref:    root + MapPath,
	}
	pageChrome := chrome{SiteTitle: title, Home: prefix(opts.BaseURL, 1) + IndexPath, MapHref: prefix(opts.BaseURL, 1) + MapPath}

	taken := map[string]bool{}
	slugs := make([]string, len(entries))
	for i, entry := range entries {
		if entry.Workflow == nil {
			return nil, fmt.Errorf("render: site entry has no workflow")
		}
		slugs[i] = uniqueSlug(entry, taken)
	}
	graph := newSiteGraph(entries, slugs)
	pageHref := func(slug string) string { return prefix(opts.BaseURL, 1) + path.Join(ShortcutsDirname, slug+".html") }
	rootHref := func(slug string) string { return root + path.Join(ShortcutsDirname, slug+".html") }

	rows := make([]indexRow, 0, len(entries))
	var latest time.Time
	for i, entry := range entries {
		slug := slugs[i]
		deps := graph.pageDeps(i, pageHref, prefix(opts.BaseURL, 1)+MapPath)
		rendered := buildPage(entry.Workflow, opts.ExpandAll, deps)
		rendered.Stylesheet = prefix(opts.BaseURL, 1) + StylesheetPath
		rendered.Home = pageChrome.Home
		rendered.Chrome = pageChrome
		body, err := execute(pages, "review", rendered)
		if err != nil {
			return nil, err
		}
		out[path.Join(ShortcutsDirname, slug+".html")] = body

		modified := entry.Workflow.Modified
		if modified.After(latest) {
			latest = modified
		}
		index.Actions += len(entry.Workflow.Actions)
		if rendered.Header.Corrupted {
			index.Flagged++
		}
		row := indexRow{
			Name:         entry.Workflow.Name,
			Href:         rootHref(slug),
			ActionCount:  len(entry.Workflow.Actions),
			MinimumOS:    rendered.Header.MinimumOS,
			ModifiedDate: formatDate(modified),
			ModifiedSort: sortableTime(modified),
			Color:        rendered.Header.Color,
			ColorText:    rendered.Header.ColorText,
			DarkIcon:     rendered.Header.DarkIcon,
			Flagged:      rendered.Header.Corrupted,
			Calls:        graph.outgoing(i),
			Callers:      len(deps.CalledBy),
			Missing:      graph.missing(i),
		}
		if graph.connected(i) {
			index.Connected++
		} else {
			index.Standalone++
		}
		if row.Missing > 0 {
			index.Missing++
		}
		rows = append(rows, row)
	}
	index.Count = len(rows)
	index.LastModified = formatDate(latest)

	sort.SliceStable(rows, func(i, j int) bool {
		return lessName(rows[i].Name, rows[i].Href, rows[j].Name, rows[j].Href)
	})
	index.Rows = rows

	body, err := execute(pages, "index", index)
	if err != nil {
		return nil, err
	}
	out[IndexPath] = body

	dependencies := graph.mapPage(rootHref)
	dependencies.Title = title
	dependencies.Stylesheet = root + StylesheetPath
	dependencies.Chrome = index.Chrome
	dependencies.Chrome.OnMap = true
	dependencies.Home = index.Chrome.Home
	body, err = execute(pages, "dependencies", dependencies)
	if err != nil {
		return nil, err
	}
	out[MapPath] = body
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
