package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strconv"
	"sync"
)

//go:embed data/*.json
var data embed.FS

type Catalog struct {
	actions    map[string][]Action
	conditions map[string]string
	filters    map[string]FilterProperty
	units      map[string]map[int]string
}

type FilterProperty struct {
	Type    string
	Default string
	Unit    string
}

var defaultCatalog = sync.OnceValues(Load)

func Default() *Catalog {
	c, err := defaultCatalog()
	if err != nil {
		panic("catalog: embedded data is invalid: " + err.Error())
	}
	return c
}

func Load() (*Catalog, error) {
	c := &Catalog{}

	var rawActions map[string][]rawState
	if err := unmarshalFile("data/actions.json", &rawActions); err != nil {
		return nil, err
	}
	c.actions = make(map[string][]Action, len(rawActions))
	for identifier, states := range rawActions {
		parsed := make([]Action, len(states))
		for mode, state := range states {
			action, err := state.action(identifier, mode, len(states) > 1)
			if err != nil {
				return nil, fmt.Errorf("actions.json: %s mode %d: %w", identifier, mode, err)
			}
			parsed[mode] = action
		}
		c.actions[identifier] = parsed
	}

	var rawConditions map[string]string
	if err := unmarshalFile("data/conditions.json", &rawConditions); err != nil {
		return nil, err
	}
	c.conditions = make(map[string]string, len(rawConditions))
	for code, htmlEscapedOperator := range rawConditions {
		c.conditions[code] = html.UnescapeString(htmlEscapedOperator)
	}

	var rawFilters map[string]struct {
		Type    string `json:"type"`
		Default string `json:"default"`
		Unit    string `json:"unit"`
	}
	if err := unmarshalFile("data/filters.json", &rawFilters); err != nil {
		return nil, err
	}
	c.filters = make(map[string]FilterProperty, len(rawFilters))
	for property, f := range rawFilters {
		c.filters[property] = FilterProperty{Type: f.Type, Default: f.Default, Unit: f.Unit}
	}

	var rawUnits map[string]map[string]string
	if err := unmarshalFile("data/units.json", &rawUnits); err != nil {
		return nil, err
	}
	c.units = make(map[string]map[int]string, len(rawUnits))
	for enumeration, codes := range rawUnits {
		decoded := make(map[int]string, len(codes))
		for code, unit := range codes {
			n, err := strconv.Atoi(code)
			if err != nil {
				return nil, fmt.Errorf("units.json: %s: non-numeric code %q", enumeration, code)
			}
			decoded[n] = unit
		}
		c.units[enumeration] = decoded
	}

	return c, nil
}

func unmarshalFile(name string, v any) error {
	b, err := data.ReadFile(name)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("catalog: %s: %w", name, err)
	}
	return nil
}

func (c *Catalog) Lookup(identifier string, mode int) (Action, bool) {
	states, ok := c.actions[identifier]
	if !ok || len(states) == 0 {
		return Action{}, false
	}
	if mode < 0 || mode >= len(states) || states[mode].unusedMode() {
		mode = 0
	}
	return states[mode], true
}

func (c *Catalog) Modes(identifier string) int {
	return len(c.actions[identifier])
}

func (c *Catalog) Condition(code int) (string, bool) {
	operator, ok := c.conditions[strconv.Itoa(code)]
	return operator, ok
}

func (c *Catalog) Unit(enumeration string, code int) (string, bool) {
	unit, ok := c.units[enumeration][code]
	return unit, ok
}

func (c *Catalog) Filter(property string) (FilterProperty, bool) {
	f, ok := c.filters[property]
	return f, ok
}

func (c *Catalog) Identifiers() []string {
	ids := make([]string, 0, len(c.actions))
	for id := range c.actions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

var minimumOSVersions = []struct {
	clientVersion int
	osVersion     string
}{
	{467, "12"},
	{900, "13"},
	{1106, "15"},
	{1113, "15"},
	{2025, "17"},
	{3010, "18"},
	{3216, "18.2"},
	{5001, "27"},
	{5023, "27"},
}

func MinimumOSVersion(clientVersion int) (string, bool) {
	next := sort.Search(len(minimumOSVersions), func(i int) bool {
		return minimumOSVersions[i].clientVersion > clientVersion
	})
	if next == 0 {
		return "", false
	}
	return minimumOSVersions[next-1].osVersion, true
}

var structuralParameters = []string{"UUID", "GroupingIdentifier", "WFControlFlowMode", "AppIntentDescriptor"}

func IgnoredParameters() []string {
	return append([]string(nil), structuralParameters...)
}

func IsIgnoredParameter(key string) bool {
	for _, structural := range structuralParameters {
		if structural == key {
			return true
		}
	}
	return false
}
