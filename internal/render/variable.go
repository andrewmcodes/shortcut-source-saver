package render

import (
	"fmt"
	"strconv"
	"strings"
)

const maxVariableWrappers = 8

var variableKindLabels = map[string]string{
	"Ask":            "Ask Each Time",
	"Clipboard":      "Clipboard",
	"CurrentDate":    "Current Date",
	"DeviceDetails":  "Device Details",
	"ExtensionInput": "Shortcut Input",
}

func unwrapVariable(v any) any {
	for depth := 0; depth < maxVariableWrappers; depth++ {
		value, ok := v.(map[string]any)
		if !ok {
			return v
		}
		switch {
		case stringOr(value, "WFSerializationType") == "WFTextTokenAttachment":
			v = value["Value"]
		case stringOr(value, "Type") == "Variable" && stringOr(value, "VariableName") == "":
			nested, ok := value["Variable"]
			if !ok {
				return value
			}
			v = nested
		default:
			return value
		}
	}
	return v
}

func (r *reviewer) variable(v any) []node {
	unwrapped := unwrapVariable(v)
	value, ok := unwrapped.(map[string]any)
	if !ok {
		if s, ok := formatScalar(unwrapped); ok {
			return []node{boxOf(visibleText(s))}
		}
		return r.value(unwrapped)
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
		name := kind
		if label, ok := variableKindLabels[kind]; ok {
			name = label
		}
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
