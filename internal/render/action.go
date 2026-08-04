package render

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/andrewmcodes/shortcut-source-saver/internal/catalog"
)

const (
	conditionalIdentifier = "is.workflow.actions.conditional"
	menuIdentifier        = "is.workflow.actions.choosefrommenu"
	commentIdentifier     = "is.workflow.actions.comment"
	outputNameParameter   = "CustomOutputName"
	operationParameter    = "operation"
)

const (
	bigBodyCharacters = 10000
	bigBodyNewlines   = 80
	bigRowCharacters  = 1000
	backLinkDistance  = 5
	summaryLimit      = 120
)

type paramRow struct {
	Label string
	Value []node
	Full  bool
}

type item struct {
	Index        int
	Label        []node
	Summary      string
	Rows         []paramRow
	Group        string
	Kind         flowKind
	Comment      bool
	Big          bool
	Container    bool
	Collapsed    bool
	ButtonClass  string
	ContentClass string
	Children     []*item
}

func (r *reviewer) renderAction(in actionInput) *item {
	r.current = in.Index
	act, catalogued := r.cat.Lookup(in.Identifier, in.Mode)
	params := copyParams(in.Params)

	name := act.Name
	group := string(act.Group)
	if !catalogued {
		name = catalog.DisplayName(in.Identifier)
		group = string(catalog.FallbackGroup(in.Identifier))
	}

	if in.Identifier == menuIdentifier && in.Mode == controlFlowBranch {
		if title, ok := params["WFMenuItemAttributedTitle"]; ok {
			params["WFMenuItemTitle"] = title
			delete(params, "WFMenuItemAttributedTitle")
		}
	}

	consumed := map[string]bool{}
	subs := map[catalog.Slot][]node{}
	var rows []paramRow
	var suffix []node
	big := false

	if in.Identifier == conditionalIdentifier && in.Mode < controlFlowEnd {
		suffix = append(suffix, r.conditionBlock(params, consumed, in.Mode)...)
	}
	if act.HasSlot(catalog.SlotToggle) {
		rows = append(rows, r.applyToggle(act, params, name, subs, consumed)...)
	}
	r.applyEntities(act, params, subs)

	for _, key := range sortedKeys(params) {
		if catalog.IsIgnoredParameter(key) || consumed[key] {
			continue
		}
		value := params[key]

		if act.Body != "" && key == act.Body {
			nodes := r.value(value)
			body := plainText(nodes)
			if len(body) > bigBodyCharacters || countNewlines(body) > bigBodyNewlines {
				big = true
			}
			rows = append(rows, paramRow{Full: true, Value: nodes})
			continue
		}
		if key == outputNameParameter {
			if label, ok := formatScalar(value); ok {
				marker := append([]node{text(continuationSpace + "➔ ")}, visibleText(label)...)
				suffix = append(suffix, wrap(kindBold, marker...))
			}
			continue
		}
		if slot, ok := act.SlotFor(key); ok {
			if slot != catalog.SlotToggle {
				subs[slot] = []node{boxOf(r.value(value))}
			}
			continue
		}

		nodes := r.value(value)
		if len(plainText(nodes)) > bigRowCharacters {
			big = true
		}
		rows = append(rows, paramRow{Label: strings.TrimPrefix(key, "WF"), Value: nodes})
	}

	title := append(fillSlots(name, subs, act.Defaults), suffix...)
	summary := summarize(title)
	if in.Corrupted {
		title = []node{flagged(append(title, text(" (corrupted block)"))...)}
	}
	title = append(title, r.blockLink(in)...)

	label := append([]node{text(strconv.Itoa(in.Index) + " ")}, title...)
	return &item{
		Index:   in.Index,
		Label:   label,
		Summary: summary,
		Rows:    rows,
		Group:   group,
		Kind:    in.Kind,
		Comment: in.Identifier == commentIdentifier,
		Big:     big,
	}
}

func (r *reviewer) blockLink(in actionInput) []node {
	target := -1
	switch {
	case in.OpenerIndex >= 0 && in.OpenerIndex < in.Index-backLinkDistance:
		target = in.OpenerIndex
	case in.ClosedAt >= 0:
		target = in.ClosedAt
	}
	if target < 0 {
		return nil
	}
	return []node{text(continuationSpace + "▵ "), magic(target, text(strconv.Itoa(target)))}
}

func (r *reviewer) applyToggle(act catalog.Action, params map[string]any, name string,
	subs map[catalog.Slot][]node, consumed map[string]bool) []paramRow {
	stored, present := params[act.Toggle.Key]
	consumed[act.Toggle.Key] = true

	if toggleReadsInline(name, params) {
		if !present {
			subs[catalog.SlotToggle] = textNodes(act.Toggle.Text(""))
			return nil
		}
		subs[catalog.SlotToggle] = []node{boxOf(r.toggleValue(act.Toggle, stored))}
		return nil
	}

	subs[catalog.SlotToggle] = nil
	if !present {
		return nil
	}
	return []paramRow{{Label: act.Toggle.Key, Value: r.toggleValue(act.Toggle, stored)}}
}

func toggleReadsInline(name string, params map[string]any) bool {
	if strings.HasPrefix(name, "Turn ") {
		return true
	}
	operation, ok := stringAt(params, operationParameter)
	return !ok || operation == "turn" || operation == "set"
}

func (r *reviewer) toggleValue(toggle catalog.Toggle, stored any) []node {
	if nested, ok := stored.(map[string]any); ok {
		return r.value(nested)
	}
	raw, _ := formatScalar(stored)
	if label := toggle.Text(raw); label != "" {
		return textNodes(label)
	}
	return visibleText(raw)
}

func (r *reviewer) applyEntities(act catalog.Action, params map[string]any, subs map[catalog.Slot][]node) {
	for _, slot := range []catalog.Slot{catalog.SlotN(7), catalog.SlotN(8)} {
		entity, ok := act.Entities[slot]
		if !ok || len(entity.Path) == 0 {
			continue
		}
		subs[slot] = r.entityValue(params, entity)
	}
}

func (r *reviewer) entityValue(params map[string]any, entity catalog.Entity) []node {
	switch root := params[entity.Parameter()].(type) {
	case []any:
		var out []node
		for i, element := range root {
			if i > 0 {
				out = append(out, text(" "))
			}
			out = append(out, boxOf(r.pathValue(element, entity.Path[1:])))
		}
		if len(out) > 0 {
			return out
		}
	case map[string]any:
		if _, ok := root["Value"]; ok {
			return []node{boxOf(r.value(root))}
		}
	}
	if nodes := r.pathValue(params, entity.Path); len(nodes) > 0 {
		return []node{boxOf(nodes)}
	}
	if entity.Fallback != "" {
		return []node{box(text(entity.Fallback))}
	}
	return []node{box()}
}

func (r *reviewer) pathValue(value any, path []string) []node {
	for _, key := range path {
		holder, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = holder[key]
	}
	if value == nil {
		return nil
	}
	if scalar, ok := formatScalar(value); ok {
		if scalar == "" {
			return nil
		}
		return visibleText(scalar)
	}
	return r.value(value)
}

func fillSlots(name string, subs map[catalog.Slot][]node, defaults map[catalog.Slot]string) []node {
	var out []node
	var literal strings.Builder
	runes := []rune(name)

	flush := func() {
		if literal.Len() > 0 {
			out = append(out, text(literal.String()))
			literal.Reset()
		}
	}

	for i := 0; i < len(runes); i++ {
		if runes[i] != '$' || i+2 >= len(runes) || runes[i+1] != objectReplacement || !unicode.IsDigit(runes[i+2]) {
			literal.WriteRune(runes[i])
			continue
		}
		slot := catalog.Slot(string(runes[i : i+3]))
		i += 2
		flush()
		if nodes, ok := subs[slot]; ok {
			out = append(out, nodes...)
			continue
		}
		if fallback, ok := defaults[slot]; ok {
			out = append(out, text(fallback))
			continue
		}
		out = append(out, box())
	}
	flush()
	return out
}

func summarize(nodes []node) string {
	summary := strings.Join(strings.FieldsFunc(markdownText(nodes), func(r rune) bool {
		return r == '\n' || r == '\t' || r == ' ' || r == '　'
	}), " ")
	if len([]rune(summary)) > summaryLimit {
		summary = string([]rune(summary)[:summaryLimit]) + "…"
	}
	return summary
}

func copyParams(params map[string]any) map[string]any {
	out := make(map[string]any, len(params))
	for key, value := range params {
		out[key] = value
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
