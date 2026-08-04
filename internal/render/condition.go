package render

import (
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/catalog"
)

const (
	conditionHasAnyValue      = 100
	conditionHasNoValue       = 101
	conditionIsToday          = 1002
	conditionIsBetween        = 1003
	conditionRelativeBoundary = 999
)

var conditionOperandKeys = []string{
	"WFConditionalActionString", "WFNumberValue", "WFMeasurement", "WFDuration",
	"WFDate", "WFEnumeration", "WFLinkEnumeration",
}

var conditionOperandPairs = [][2]string{
	{"WFNumberValue", "WFAnotherNumber"},
	{"WFDate", "WFAnotherDate"},
	{"WFDuration", "WFAnotherDuration"},
	{"WFMeasurement", "WFAnotherMeasurement"},
}

func (r *reviewer) conditionBlock(params map[string]any, consumed map[string]bool, mode int) []node {
	if _, ok := params["WFCondition"]; !ok {
		return nil
	}

	var out []node
	if mode == controlFlowBranch {
		out = append(out, text(" if "))
	}
	consumed["WFCondition"] = true

	if group, ok := params["WFConditions"].(map[string]any); ok {
		consumed["WFConditions"] = true
		return append(out, r.conditionGroup(group)...)
	}
	return append(out, r.singleCondition(params, consumed)...)
}

func (r *reviewer) conditionGroup(group map[string]any) []node {
	value, _ := group["Value"].(map[string]any)
	prefix, _ := intAt(value, "WFActionParameterFilterPrefix")
	quantifier := "All"
	if prefix == 0 {
		quantifier = "Any"
	}
	out := []node{box(text(quantifier)), text(" are true")}

	templates, _ := value["WFActionParameterFilterTemplates"].([]any)
	for _, raw := range templates {
		template, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, lineBreak(), text(continuationSpace))
		out = append(out, r.singleCondition(template, map[string]bool{})...)
	}
	return out
}

func (r *reviewer) singleCondition(params map[string]any, consumed map[string]bool) []node {
	code, _ := intAt(params, "WFCondition")
	operator, _ := r.cat.Condition(code)

	out := []node{boxOf(r.value(params["WFInput"]))}
	consumed["WFInput"] = true
	out = append(out, text(" "), box(text(operator)))

	switch code {
	case conditionHasAnyValue, conditionHasNoValue, conditionIsToday:
		return out
	case conditionIsBetween:
		for _, pair := range conditionOperandPairs {
			first, hasFirst := params[pair[0]]
			second, hasSecond := params[pair[1]]
			if !hasFirst || !hasSecond {
				continue
			}
			consumed[pair[0]] = true
			consumed[pair[1]] = true
			return append(out, text(" "), boxOf(r.value(first)), text(" and "), boxOf(r.value(second)))
		}
		return out
	}

	var present []string
	for _, key := range conditionOperandKeys {
		if _, ok := params[key]; ok {
			present = append(present, key)
		}
	}
	switch len(present) {
	case 0:
		return out
	case 1:
		consumed[present[0]] = true
		return append(out, text(" "), boxOf(r.value(params[present[0]])))
	}

	var operands []node
	for i, key := range present {
		consumed[key] = true
		if i > 0 {
			operands = append(operands, lineBreak())
		}
		operands = append(operands, text(strings.TrimPrefix(key, "WF")+" "))
		operands = append(operands, r.value(params[key])...)
	}
	return append(out, text(" "), boxOf(operands))
}

func (r *reviewer) predicate(v any) []node {
	value, ok := v.(map[string]any)
	if !ok {
		return r.value(v)
	}
	filters, _ := value["WFActionParameterFilterTemplates"].([]any)
	if len(filters) == 0 {
		return r.json(v)
	}

	var out []node
	if len(filters) > 1 {
		prefix, _ := intAt(value, "WFActionParameterFilterPrefix")
		quantifier := "All"
		if prefix == 0 {
			quantifier = "Any of"
		}
		out = append(out, text("• "+quantifier+" the following are true\n"))
	}
	for i, raw := range filters {
		filter, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if i > 0 {
			out = append(out, text("\n"))
		}
		out = append(out, r.filter(filter)...)
	}
	return out
}

func (r *reviewer) filter(filter map[string]any) []node {
	property := stringOr(filter, "Property")
	values, _ := filter["Values"].(map[string]any)
	operatorCode, _ := intAt(filter, "Operator")
	operator, _ := r.cat.Condition(operatorCode)

	shape := r.filterShape(property)
	if shape.Type == "Bool" {
		if enabled, ok := values["Bool"].(bool); ok && !enabled {
			return visibleText(strings.Replace(property, " ", " Not ", 1))
		}
		return visibleText(property)
	}

	out := visibleText(property + " " + operator)
	switch {
	case operatorCode == conditionIsToday:
		return out
	case operatorCode == conditionIsBetween:
		out = append(out, text(" "))
		out = append(out, r.filterOperand(values, "Date")...)
		out = append(out, text(" and "))
		return append(out, r.filterOperand(values, "AnotherDate")...)
	case operatorCode > conditionRelativeBoundary && shape.Type == "Date":
		shape = catalog.FilterProperty{Type: "Number", Unit: "Unit"}
	}

	out = append(out, text(" "))
	out = append(out, r.filterOperand(values, shape.Type)...)
	if shape.Unit != "" {
		out = append(out, text(" "+r.filterUnit(shape, values)))
	}
	return out
}

func (r *reviewer) filterShape(property string) catalog.FilterProperty {
	if shape, ok := r.cat.Filter(property); ok {
		return shape
	}
	switch {
	case strings.Contains(property, "Date") || property == "Birthday":
		return catalog.FilterProperty{Type: "Date"}
	case strings.HasPrefix(property, "Is ") || strings.HasPrefix(property, "Has "):
		return catalog.FilterProperty{Type: "Bool"}
	}
	return catalog.FilterProperty{Type: "String"}
}

func (r *reviewer) filterOperand(values map[string]any, key string) []node {
	if value, ok := values[key]; ok {
		return r.value(value)
	}
	if value, ok := values["Number"]; ok {
		return r.value(value)
	}
	if key == "Date" || key == "AnotherDate" {
		return textNodes("(now)")
	}
	return textNodes("(default)")
}

const (
	defaultDurationUnit = 32
	defaultRangeUnit    = 4
)

func (r *reviewer) filterUnit(shape catalog.FilterProperty, values map[string]any) string {
	code := defaultRangeUnit
	if shape.Type == "Duration" {
		code = defaultDurationUnit
	}
	if raw, ok := values[shape.Unit]; ok {
		if nested, ok := raw.(map[string]any); ok {
			if n, ok := intAt(nested, "Value"); ok {
				code = n
			}
		} else if n, ok := toInt(raw); ok {
			code = n
		}
	}
	unit, _ := r.cat.Unit(shape.Unit, code)
	return unit
}
