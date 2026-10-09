package render

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
)

const dictIndent = "   "

func (r *reviewer) value(v any) []node {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return visibleText(t)
	case bool:
		return textNodes(strconv.FormatBool(t))
	case time.Time:
		return textNodes(t.UTC().Format(timestampLayout))
	case []byte:
		return r.data(t)
	case []any:
		return r.list(t)
	case map[string]any:
		return r.mapping(t)
	default:
		if s, ok := formatScalar(v); ok {
			return textNodes(s)
		}
		return textNodes(fmt.Sprint(v))
	}
}

func (r *reviewer) data(b []byte) []node {
	if utf8.Valid(b) {
		return visibleText(string(b))
	}
	return []node{italic(fmt.Sprintf("(%d bytes of data)", len(b)))}
}

func (r *reviewer) list(items []any) []node {
	if len(items) == 0 {
		return textNodes("[]")
	}
	out := []node{text("[")}
	for i, item := range items {
		if i > 0 {
			out = append(out, text(",\n"))
		}
		out = append(out, r.value(item)...)
	}
	return append(out, text("]"))
}

func (r *reviewer) mapping(m map[string]any) []node {
	if kind, ok := stringAt(m, "Type"); ok {
		if kind == "Variable" || kind == "ActionOutput" || variableKindLabels[kind] != "" {
			return r.variable(m)
		}
		return textNodes("?type?" + kind)
	}
	if _, ok := intAt(m, "WFItemType"); ok {
		return r.dictionaryItemValue(m)
	}

	serialization, _ := stringAt(m, "WFSerializationType")
	switch serialization {
	case "WFTextTokenAttachment":
		return r.variable(m["Value"])
	case "WFTextTokenString":
		return r.tokenString(m["Value"])
	case "WFDictionaryFieldValue":
		return r.dictionary(m["Value"])
	case "WFQuantityFieldValue":
		return r.quantity(m["Value"])
	case "WFINSpeakableStringSubstitutableState":
		if value, ok := m["Value"].(map[string]any); ok {
			display, _ := stringAt(value, "DisplayString")
			return visibleText(display)
		}
		return nil
	case "WFNumberSubstitutableState", "WFNumberStringSubstitutableState", "WFDateSubstitutableState", "WFStringSubstitutableState":
		return r.value(m["Value"])
	case "WFContentPredicateTableTemplate":
		return r.predicate(m["Value"])
	case "WFContactFieldValue":
		if value, ok := m["Value"].(map[string]any); ok {
			if fields, ok := value["WFContactFieldValues"].([]any); ok {
				return r.list(fields)
			}
		}
		return r.json(m)
	}

	if _, ok := m["string"]; ok {
		return r.tokenString(m)
	}
	return r.json(m)
}

func (r *reviewer) quantity(v any) []node {
	value, ok := v.(map[string]any)
	if !ok {
		return r.value(v)
	}
	out := r.value(value["Magnitude"])
	if unit, ok := stringAt(value, "Unit"); ok {
		out = append(out, text(" "+unit))
	}
	return out
}

func (r *reviewer) json(v any) []node {
	data, err := archive.CanonicalJSON(v)
	if err != nil {
		return textNodes(fmt.Sprint(v))
	}
	return visibleText(strings.TrimRight(string(data), "\n"))
}

func (r *reviewer) dictionary(v any) []node {
	value, ok := v.(map[string]any)
	if !ok {
		return r.value(v)
	}
	items, _ := value["WFDictionaryFieldValueItems"].([]any)
	if len(items) == 0 {
		return textNodes("{}")
	}

	out := []node{text("{\n")}
	for i, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if i > 0 {
			out = append(out, text(",\n"))
		}
		out = append(out, text(dictIndent))
		entry := append([]node{}, indentNodes(r.dictionaryKey(item), dictIndent)...)
		entry = append(entry, text(": "))
		entry = append(entry, indentNodes(r.dictionaryItemValue(item), dictIndent)...)
		out = append(out, entry...)
	}
	return append(out, text("\n}"))
}

func (r *reviewer) dictionaryKey(item map[string]any) []node {
	key, ok := item["WFKey"]
	if !ok {
		return textNodes(`""`)
	}
	return append(append([]node{text(`"`)}, r.value(key)...), text(`"`))
}

func (r *reviewer) dictionaryItemValue(item map[string]any) []node {
	itemType, _ := intAt(item, "WFItemType")
	holder, _ := item["WFValue"].(map[string]any)
	if holder == nil {
		return r.value(item["WFValue"])
	}
	inner := holder["Value"]
	substitutable := strings.Contains(stringOr(holder, "WFSerializationType"), "Substitutable")

	switch itemType {
	case 0:
		return r.value(holder)
	case 1:
		if substitutable {
			return append(r.substitutableVariable(inner), italic(" as Dictionary"))
		}
		return r.dictionary(inner)
	case 2:
		if substitutable {
			return append(r.substitutableVariable(inner), italic(" as Array"))
		}
		items, _ := inner.([]any)
		if len(items) == 0 {
			return textNodes("[]")
		}
		out := []node{text("[\n" + dictIndent)}
		for i, raw := range items {
			if i > 0 {
				out = append(out, text(",\n"+dictIndent))
			}
			entry, _ := raw.(map[string]any)
			if entry == nil {
				out = append(out, indentNodes(r.value(raw), dictIndent)...)
				continue
			}
			out = append(out, indentNodes(r.dictionaryItemValue(entry), dictIndent)...)
		}
		return append(out, text("\n]"))
	case 3:
		return append(r.value(holder), italic(" as Number"))
	case 4:
		return append(r.value(holder), italic(" as Boolean"))
	case 5:
		return r.substitutableVariable(inner)
	}
	return r.value(holder)
}

func (r *reviewer) substitutableVariable(inner any) []node {
	if m, ok := inner.(map[string]any); ok {
		if nested, ok := m["Value"]; ok {
			return r.variable(nested)
		}
	}
	return r.value(inner)
}

func (r *reviewer) tokenString(v any) []node {
	value, ok := v.(map[string]any)
	if !ok {
		return r.value(v)
	}
	body, _ := stringAt(value, "string")
	attachments := sortedAttachments(value["attachmentsByRange"])
	return r.spliceAttachments(visibleText(body), attachments)
}

func (r *reviewer) spliceAttachments(nodes []node, attachments []any) []node {
	used := 0
	out := make([]node, 0, len(nodes)+2*len(attachments))
	for _, current := range nodes {
		if current.Kind != kindText || !strings.ContainsRune(current.Text, objectReplacement) {
			out = append(out, current)
			continue
		}
		for i, segment := range strings.Split(current.Text, string(objectReplacement)) {
			if i > 0 {
				if used < len(attachments) {
					out = append(out, r.variable(attachments[used])...)
					used++
				} else {
					out = append(out, text(string(objectReplacement)))
				}
			}
			if segment != "" {
				out = append(out, text(segment))
			}
		}
	}
	return out
}

type attachmentPlacement struct {
	offset int
	key    string
	value  any
}

func sortedAttachments(v any) []any {
	byRange, _ := v.(map[string]any)
	if len(byRange) == 0 {
		return nil
	}
	placements := make([]attachmentPlacement, 0, len(byRange))
	for key, value := range byRange {
		placements = append(placements, attachmentPlacement{offset: rangeOffset(key), key: key, value: value})
	}
	sort.Slice(placements, func(i, j int) bool {
		if placements[i].offset != placements[j].offset {
			return placements[i].offset < placements[j].offset
		}
		return placements[i].key < placements[j].key
	})

	out := make([]any, len(placements))
	for i, p := range placements {
		out[i] = p.value
	}
	return out
}

func rangeOffset(key string) int {
	digits := strings.IndexFunc(key, func(r rune) bool { return r >= '0' && r <= '9' })
	if digits < 0 {
		return 0
	}
	end := digits
	for end < len(key) && key[end] >= '0' && key[end] <= '9' {
		end++
	}
	offset, err := strconv.Atoi(key[digits:end])
	if err != nil {
		return 0
	}
	return offset
}

func stringAt(m map[string]any, key string) (string, bool) {
	s, ok := m[key].(string)
	return s, ok
}

func stringOr(m map[string]any, key string) string {
	s, _ := stringAt(m, key)
	return s
}

func intAt(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	return toInt(m[key])
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func formatScalar(v any) (string, bool) {
	switch n := v.(type) {
	case string:
		return n, true
	case bool:
		return strconv.FormatBool(n), true
	case float32:
		return strconv.FormatFloat(float64(n), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64), true
	}
	if i, ok := toInt(v); ok {
		return strconv.Itoa(i), true
	}
	return "", false
}
