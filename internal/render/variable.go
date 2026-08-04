package render

import (
	"fmt"
	"strconv"
	"strings"
)

func (r *reviewer) variable(v any) []node {
	value, ok := v.(map[string]any)
	if !ok {
		if s, ok := formatScalar(v); ok {
			return []node{boxOf(visibleText(s))}
		}
		return r.value(v)
	}

	if stringOr(value, "WFSerializationType") == "WFTextTokenAttachment" {
		return r.variable(value["Value"])
	}

	kind := stringOr(value, "Type")
	switch kind {
	case "Variable":
		name := stringOr(value, "VariableName")
		return []node{wrap(kindVar, r.tokenBody(name, value)...)}
	case "ActionOutput":
		uuid := stringOr(value, "OutputUUID")
		producer, known := r.producers[uuid]
		label := stringOr(value, "OutputName")
		if !known {
			return []node{flagged(wrap(kindVar, r.tokenBody("? "+label, value)...))}
		}
		r.recordConsumer(producer)
		return []node{magic(producer, r.tokenBody(strconv.Itoa(producer)+" "+label, value)...)}
	default:
		name := strings.ReplaceAll(kind, "ExtensionInput", "Shortcut Input")
		if prompt := stringOr(value, "Prompt"); prompt != "" {
			name += " “" + prompt + "”"
		}
		return []node{wrap(kindVar, r.tokenBody(name, value)...)}
	}
}

func (r *reviewer) tokenBody(name string, value map[string]any) []node {
	inner := append([]node{text("[")}, visibleText(name)...)
	modifiers := r.aggrandizements(value["Aggrandizements"])
	if len(modifiers) == 0 {
		return []node{wrap(kindBold, append(inner, text("]"))...)}
	}
	out := []node{wrap(kindBold, inner...)}
	out = append(out, modifiers...)
	return append(out, bold("]"))
}

func (r *reviewer) aggrandizements(v any) []node {
	list, _ := v.([]any)
	var out []node
	for _, raw := range list {
		a, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch stringOr(a, "Type") {
		case "WFCoercionVariableAggrandizement":
			out = append(out, text(" as "+coercionName(stringOr(a, "CoercionItemClass"))))
		case "WFDictionaryValueVariableAggrandizement":
			out = append(out, visibleText("."+stringOr(a, "DictionaryKey"))...)
		case "WFPropertyVariableAggrandizement":
			name := stringOr(a, "NegativePropertyName")
			if name == "" {
				name = stringOr(a, "PropertyName")
			}
			out = append(out, visibleText("’s "+name)...)
		default:
			out = append(out, visibleText(" "+describeAggrandizement(a))...)
		}
	}
	return out
}

func coercionName(itemClass string) string {
	name := strings.TrimPrefix(itemClass, "WF")
	name = strings.TrimSuffix(name, "ContentItem")
	name = strings.ReplaceAll(name, "String", "Text")
	name = strings.ReplaceAll(name, "Workflow", "Shortcut")
	if name == "" {
		return "Anything"
	}
	return name
}

var aggrandizementLabels = map[string]string{
	"WFDateFormatStyle":         "Date Style",
	"WFTimeFormatStyle":         "Time Style",
	"WFRelativeDateFormatStyle": "Relative Style",
	"WFISO8601IncludeTime":      "ISO 8601 Time",
}

func describeAggrandizement(a map[string]any) string {
	var parts []string
	for _, key := range sortedKeys(a) {
		if key == "Type" {
			continue
		}
		label := key
		if pretty, ok := aggrandizementLabels[key]; ok {
			label = pretty
		}
		value, ok := formatScalar(a[key])
		if !ok {
			value = fmt.Sprint(a[key])
		}
		parts = append(parts, label+" "+value)
	}
	return strings.Join(parts, " ")
}
