package catalog

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const objectReplacementCharacter = "￼"

const slotPrefix = "$" + objectReplacementCharacter

type Slot string

const SlotToggle Slot = slotPrefix + "9"

func SlotN(n int) Slot {
	return Slot(slotPrefix + strconv.Itoa(n))
}

func (s Slot) Index() (int, bool) {
	digits, ok := strings.CutPrefix(string(s), slotPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (s Slot) isEntity() bool {
	return s == SlotN(7) || s == SlotN(8)
}

func (s Slot) isEntityFallback() bool {
	return s == SlotN(70) || s == SlotN(80)
}

func (s Slot) entityForFallback() Slot {
	if s == SlotN(80) {
		return SlotN(8)
	}
	return SlotN(7)
}

type Action struct {
	Identifier  string
	Mode        int
	ControlFlow bool
	Name        string
	Group       Group
	Slots       map[string]Slot
	Defaults    map[Slot]string
	Body        string
	Toggle      Toggle
	Entities    map[Slot]Entity
}

const primaryInputParameter = "WFInput"

func (a Action) SlotFor(parameter string) (Slot, bool) {
	if slot, ok := a.Slots[parameter]; ok {
		return slot, true
	}
	if parameter == primaryInputParameter && a.HasSlot(SlotN(0)) && !a.slotIsClaimed(SlotN(0)) {
		return SlotN(0), true
	}
	return "", false
}

func (a Action) HasSlot(slot Slot) bool {
	return strings.Contains(a.Name, string(slot))
}

func (a Action) slotIsClaimed(slot Slot) bool {
	for _, claimed := range a.Slots {
		if claimed == slot {
			return true
		}
	}
	return false
}

func (a Action) unusedMode() bool {
	return a.Name == ""
}

type Toggle struct {
	Key     string
	Default string
	Values  map[string]string
}

func (t Toggle) Text(storedValue string) string {
	if storedValue == "" {
		storedValue = t.Default
	}
	return t.Values[storedValue]
}

func defaultToggle() Toggle {
	return Toggle{
		Key:     "state",
		Default: "1",
		Values: map[string]string{
			"0": "Off", "1": "On",
			"false": "Off", "true": "On",
			"off": "Off", "on": "On",
		},
	}
}

type Entity struct {
	Path     []string
	Fallback string
}

func (e Entity) Parameter() string {
	if len(e.Path) == 0 {
		return ""
	}
	return e.Path[0]
}

const bodyParameterMarker = "#"

type rawState struct {
	Name  string                     `json:"name"`
	Group string                     `json:"group"`
	Param map[string]json.RawMessage `json:"param"`
}

func (r rawState) isUnusedMode() bool {
	return r.Name == "" && r.Group == "" && r.Param == nil
}

func (r rawState) action(identifier string, mode int, controlFlow bool) (Action, error) {
	if r.isUnusedMode() {
		return Action{}, nil
	}

	a := Action{
		Identifier:  identifier,
		Mode:        mode,
		ControlFlow: controlFlow,
		Name:        r.Name,
		Group:       Group(r.Group),
		Toggle:      defaultToggle(),
	}

	for key, raw := range r.Param {
		if !strings.HasPrefix(key, "$") {
			var slotOrBodyMarker string
			if err := json.Unmarshal(raw, &slotOrBodyMarker); err != nil {
				return Action{}, fmt.Errorf("parameter %q: %w", key, err)
			}
			if slotOrBodyMarker == bodyParameterMarker {
				a.Body = key
				continue
			}
			if a.Slots == nil {
				a.Slots = make(map[string]Slot)
			}
			a.Slots[key] = Slot(slotOrBodyMarker)
			continue
		}

		slot := Slot(key)
		switch {
		case slot == SlotToggle:
			toggle, err := mergeToggle(raw)
			if err != nil {
				return Action{}, fmt.Errorf("slot %q: %w", key, err)
			}
			a.Toggle = toggle
		case slot.isEntity():
			var dottedPath string
			if err := json.Unmarshal(raw, &dottedPath); err != nil {
				return Action{}, fmt.Errorf("slot %q: %w", key, err)
			}
			entity := a.Entities[slot]
			entity.Path = strings.Split(dottedPath, ".")
			a.setEntity(slot, entity)
		case slot.isEntityFallback():
			var fallback string
			if err := json.Unmarshal(raw, &fallback); err != nil {
				return Action{}, fmt.Errorf("slot %q: %w", key, err)
			}
			target := slot.entityForFallback()
			entity := a.Entities[target]
			entity.Fallback = fallback
			a.setEntity(target, entity)
		default:
			var slotDefault string
			if err := json.Unmarshal(raw, &slotDefault); err != nil {
				return Action{}, fmt.Errorf("slot %q: %w", key, err)
			}
			if a.Defaults == nil {
				a.Defaults = make(map[Slot]string)
			}
			a.Defaults[slot] = slotDefault
		}
	}

	return a, nil
}

func (a *Action) setEntity(slot Slot, e Entity) {
	if a.Entities == nil {
		a.Entities = make(map[Slot]Entity, 2)
	}
	a.Entities[slot] = e
}

func mergeToggle(raw json.RawMessage) (Toggle, error) {
	var override struct {
		Key     string            `json:"key"`
		Default json.RawMessage   `json:"default"`
		Dict    map[string]string `json:"dict"`
	}
	if err := json.Unmarshal(raw, &override); err != nil {
		return Toggle{}, err
	}

	toggle := defaultToggle()
	if override.Key != "" {
		toggle.Key = override.Key
	}
	if len(override.Default) > 0 {
		var asString string
		if err := json.Unmarshal(override.Default, &asString); err != nil {
			asString = string(override.Default)
		}
		toggle.Default = asString
	}
	if override.Dict != nil {
		toggle.Values = override.Dict
	}
	return toggle, nil
}
