package catalog

import (
	"reflect"
	"sync"
	"testing"
)

func load(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return c
}

func TestEmbeddedDataParsesWithExpectedCounts(t *testing.T) {
	c := load(t)

	if got, want := len(c.actions), 729; got != want {
		t.Errorf("identifiers = %d, want %d", got, want)
	}
	if got, want := len(c.conditions), 17; got != want {
		t.Errorf("conditions = %d, want %d", got, want)
	}
	if got, want := len(c.filters), 20; got != want {
		t.Errorf("filters = %d, want %d", got, want)
	}
	if got, want := len(c.units), 2; got != want {
		t.Errorf("unit enumerations = %d, want %d", got, want)
	}
	if got, want := len(c.Identifiers()), 729; got != want {
		t.Errorf("Identifiers() = %d, want %d", got, want)
	}
}

func TestEmbeddedDataHasExpectedActionsPerColourGroup(t *testing.T) {
	c := load(t)

	actionsPerGroup := map[Group]int{}
	for _, states := range c.actions {
		for _, action := range states {
			if action.unusedMode() {
				continue
			}
			actionsPerGroup[action.Group]++
		}
	}

	want := map[Group]int{
		GroupGray: 23, GroupYellow: 58, GroupBlue: 80,
		GroupOrange: 29, GroupRed: 37, GroupGreen: 42,
	}
	for group, wantCount := range want {
		if actionsPerGroup[group] != wantCount {
			t.Errorf("group %s = %d actions, want %d", group, actionsPerGroup[group], wantCount)
		}
	}
}

func TestDefaultReturnsOneSharedCatalogToConcurrentCallers(t *testing.T) {
	first := Default()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := Default(); got != first {
				t.Errorf("Default() returned a different catalog")
			}
			if _, ok := Default().Lookup("is.workflow.actions.comment", 0); !ok {
				t.Errorf("Lookup miss on a known action")
			}
		}()
	}
	wg.Wait()
}

func TestLookupResolvesControlFlowMode(t *testing.T) {
	c := load(t)

	tests := []struct {
		name       string
		identifier string
		mode       int
		found      bool
		wantName   string
		wantMode   int
		wantGroup  Group
		wantFlow   bool
	}{
		{"block start keeps its trailing space for the appended condition", "is.workflow.actions.conditional", 0, true, "If ", 0, GroupGray, true},
		{"block middle", "is.workflow.actions.conditional", 1, true, "Otherwise", 1, GroupGray, true},
		{"block end", "is.workflow.actions.conditional", 2, true, "End If", 2, GroupGray, true},
		{"menu item is the middle mode", "is.workflow.actions.choosefrommenu", 1, true, "Menu Item $￼0", 1, GroupGray, true},
		{"unused middle mode falls back to mode 0", "is.workflow.actions.repeat.count", 1, true, "Repeat $￼0 Times", 0, GroupGray, true},
		{"block end of the same action is unaffected", "is.workflow.actions.repeat.count", 2, true, "End Repeat Times", 2, GroupGray, true},
		{"mode past the range of a plain action falls back to mode 0", "is.workflow.actions.getvariable", 2, true, "Get Variable $￼0", 0, GroupOrange, false},
		{"negative mode falls back to mode 0", "is.workflow.actions.getvariable", -1, true, "Get Variable $￼0", 0, GroupOrange, false},
		{"action with no colour group", "is.workflow.actions.base64encode", 0, true, "Base64 $￼1 $￼0", 0, GroupDefault, false},
		{"uncatalogued identifier", "com.example.NotAnAction", 0, false, "", 0, GroupDefault, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, ok := c.Lookup(tt.identifier, tt.mode)
			if ok != tt.found {
				t.Fatalf("Lookup(%q, %d) found = %v, want %v", tt.identifier, tt.mode, ok, tt.found)
			}
			if !ok {
				return
			}
			if action.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", action.Name, tt.wantName)
			}
			if action.Mode != tt.wantMode {
				t.Errorf("Mode = %d, want %d", action.Mode, tt.wantMode)
			}
			if action.Group != tt.wantGroup {
				t.Errorf("Group = %q, want %q", action.Group, tt.wantGroup)
			}
			if action.ControlFlow != tt.wantFlow {
				t.Errorf("ControlFlow = %v, want %v", action.ControlFlow, tt.wantFlow)
			}
			if action.Identifier != tt.identifier {
				t.Errorf("Identifier = %q, want %q", action.Identifier, tt.identifier)
			}
		})
	}
}

func TestModesCountsControlFlowStates(t *testing.T) {
	c := load(t)

	for _, tt := range []struct {
		identifier string
		want       int
	}{
		{"is.workflow.actions.conditional", 3},
		{"is.workflow.actions.repeat.count", 3},
		{"is.workflow.actions.comment", 1},
		{"com.example.NotAnAction", 0},
	} {
		if got := c.Modes(tt.identifier); got != tt.want {
			t.Errorf("Modes(%q) = %d, want %d", tt.identifier, got, tt.want)
		}
	}
}

func TestActionExposesSlotsAndSlotDefaults(t *testing.T) {
	c := load(t)

	repeat, ok := c.Lookup("is.workflow.actions.repeat.count", 0)
	if !ok {
		t.Fatal("repeat.count not found")
	}
	if got, want := repeat.Slots["WFRepeatCount"], SlotN(0); got != want {
		t.Errorf("WFRepeatCount slot = %q, want %q", got, want)
	}
	if got, want := repeat.Defaults[SlotN(0)], "1"; got != want {
		t.Errorf("slot 0 default = %q, want %q", got, want)
	}
	if !repeat.HasSlot(SlotN(0)) {
		t.Error("HasSlot(slot 0) = false, want true")
	}
	if repeat.HasSlot(SlotToggle) {
		t.Error("HasSlot(toggle) = true, want false")
	}
}

func TestSlotForFallsBackToWFInputWhenSlotZeroIsUnclaimed(t *testing.T) {
	c := load(t)

	appendVariable, ok := c.Lookup("is.workflow.actions.appendvariable", 0)
	if !ok {
		t.Fatal("appendvariable not found")
	}
	if slot, ok := appendVariable.SlotFor("WFInput"); !ok || slot != SlotN(0) {
		t.Errorf("SlotFor(WFInput) = %q, %v; want %q, true", slot, ok, SlotN(0))
	}
	if slot, ok := appendVariable.SlotFor("WFVariableName"); !ok || slot != SlotN(1) {
		t.Errorf("SlotFor(WFVariableName) = %q, %v; want %q, true", slot, ok, SlotN(1))
	}
	if _, ok := appendVariable.SlotFor("WFNotAParameter"); ok {
		t.Error("SlotFor(unmapped parameter) = true, want false")
	}
}

func TestSlotForIgnoresWFInputWhenAnotherParameterClaimsSlotZero(t *testing.T) {
	c := load(t)

	getVariable, ok := c.Lookup("is.workflow.actions.getvariable", 0)
	if !ok {
		t.Fatal("getvariable not found")
	}
	if slot, ok := getVariable.SlotFor("WFInput"); ok {
		t.Errorf("SlotFor(WFInput) = %q, true; want false because WFVariable claims slot 0", slot)
	}
}

func TestActionBodyParameterIsNotAlsoASlot(t *testing.T) {
	c := load(t)

	for _, tt := range []struct {
		identifier string
		want       string
	}{
		{"is.workflow.actions.comment", "WFCommentActionText"},
		{"is.workflow.actions.gettext", "WFTextActionText"},
		{"is.workflow.actions.dictionary", "WFItems"},
		{"is.workflow.actions.runjavascriptonwebpage", "WFJavaScript"},
		{"is.workflow.actions.getvariable", ""},
	} {
		action, ok := c.Lookup(tt.identifier, 0)
		if !ok {
			t.Fatalf("%s not found", tt.identifier)
		}
		if action.Body != tt.want {
			t.Errorf("%s Body = %q, want %q", tt.identifier, action.Body, tt.want)
		}
		if _, ok := action.Slots[tt.want]; tt.want != "" && ok {
			t.Errorf("%s: body parameter %q also appears in Slots", tt.identifier, tt.want)
		}
	}
}

func TestActionToggleMergesCatalogOverridesOntoTheDefaultVocabulary(t *testing.T) {
	c := load(t)

	onOff := map[string]string{"0": "Off", "1": "On", "false": "Off", "true": "On", "off": "Off", "on": "On"}

	tests := []struct {
		name       string
		identifier string
		wantKey    string
		wantDefaut string
		wantValues map[string]string
	}{
		{
			name:       "no override uses the default key, default and vocabulary",
			identifier: "com.apple.AccessibilityUtilities.AXSettingsShortcuts.AXToggleAudioDescriptionsIntent",
			wantKey:    "state",
			wantDefaut: "1",
			wantValues: onOff,
		},
		{
			name:       "key only override keeps the default vocabulary",
			identifier: "is.workflow.actions.wifi.set",
			wantKey:    "OnValue",
			wantDefaut: "1",
			wantValues: onOff,
		},
		{
			name:       "numeric default override is normalised to a string",
			identifier: "is.workflow.actions.dnd.set",
			wantKey:    "Enabled",
			wantDefaut: "0",
			wantValues: onOff,
		},
		{
			name:       "vocabulary override replaces the whole map instead of merging",
			identifier: "is.workflow.actions.appearance",
			wantKey:    "style",
			wantDefaut: "dark",
			wantValues: map[string]string{"dark": "dark", "light": "light"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, ok := c.Lookup(tt.identifier, 0)
			if !ok {
				t.Fatalf("%s not found", tt.identifier)
			}
			if !action.HasSlot(SlotToggle) {
				t.Fatalf("%s has no toggle slot in %q", tt.identifier, action.Name)
			}
			if action.Toggle.Key != tt.wantKey {
				t.Errorf("Toggle.Key = %q, want %q", action.Toggle.Key, tt.wantKey)
			}
			if action.Toggle.Default != tt.wantDefaut {
				t.Errorf("Toggle.Default = %q, want %q", action.Toggle.Default, tt.wantDefaut)
			}
			if !reflect.DeepEqual(action.Toggle.Values, tt.wantValues) {
				t.Errorf("Toggle.Values = %v, want %v", action.Toggle.Values, tt.wantValues)
			}
			if got, want := action.Toggle.Text(""), tt.wantValues[tt.wantDefaut]; got != want {
				t.Errorf("Toggle.Text(missing parameter) = %q, want %q", got, want)
			}
		})
	}
}

func TestToggleTextMapsStoredValuesToWords(t *testing.T) {
	c := load(t)

	wifi, ok := c.Lookup("is.workflow.actions.wifi.set", 0)
	if !ok {
		t.Fatal("wifi.set not found")
	}
	for stored, want := range map[string]string{"0": "Off", "1": "On", "false": "Off", "true": "On"} {
		if got := wifi.Toggle.Text(stored); got != want {
			t.Errorf("Toggle.Text(%q) = %q, want %q", stored, got, want)
		}
	}
}

func TestActionEntitiesCarryDottedPathsAndFallbackLabels(t *testing.T) {
	c := load(t)

	moveShortcut, ok := c.Lookup("com.apple.shortcuts.MoveShortcutToFolderAction", 0)
	if !ok {
		t.Fatal("MoveShortcutToFolderAction not found")
	}

	folder := moveShortcut.Entities[SlotN(8)]
	if want := []string{"folder", "title", "key"}; !reflect.DeepEqual(folder.Path, want) {
		t.Errorf("slot 8 path = %v, want %v", folder.Path, want)
	}
	if got, want := folder.Fallback, "All Shortcuts"; got != want {
		t.Errorf("slot 8 fallback = %q, want %q", got, want)
	}
	if got, want := folder.Parameter(), "folder"; got != want {
		t.Errorf("slot 8 parameter = %q, want %q", got, want)
	}

	shortcuts := moveShortcut.Entities[SlotN(7)]
	if want := []string{"shortcuts", "title", "key"}; !reflect.DeepEqual(shortcuts.Path, want) {
		t.Errorf("slot 7 path = %v, want %v", shortcuts.Path, want)
	}
	if shortcuts.Fallback != "" {
		t.Errorf("slot 7 fallback = %q, want empty", shortcuts.Fallback)
	}

	getVariable, _ := c.Lookup("is.workflow.actions.getvariable", 0)
	if len(getVariable.Entities) != 0 {
		t.Errorf("getvariable entities = %v, want none", getVariable.Entities)
	}
}

func TestSlotTokens(t *testing.T) {
	if got, want := string(SlotN(0)), "$￼0"; got != want {
		t.Errorf("SlotN(0) = %q, want %q", got, want)
	}
	if got, want := SlotToggle, SlotN(9); got != want {
		t.Errorf("SlotToggle = %q, want %q", got, want)
	}
	if n, ok := SlotN(8).Index(); !ok || n != 8 {
		t.Errorf("SlotN(8).Index() = %d, %v; want 8, true", n, ok)
	}
	if _, ok := Slot("WFInput").Index(); ok {
		t.Error("Slot(\"WFInput\").Index() ok = true, want false")
	}
}

func TestConditionReturnsUnescapedOperatorText(t *testing.T) {
	c := load(t)

	for _, tt := range []struct {
		code  int
		want  string
		found bool
	}{
		{0, "<", true},
		{1, "<=", true},
		{2, ">", true},
		{3, ">=", true},
		{4, "is", true},
		{5, "is not", true},
		{99, "contains", true},
		{100, "has any value", true},
		{999, "does not contain", true},
		{1003, "is between", true},
		{42, "", false},
	} {
		got, ok := c.Condition(tt.code)
		if ok != tt.found || got != tt.want {
			t.Errorf("Condition(%d) = %q, %v; want %q, %v", tt.code, got, ok, tt.want, tt.found)
		}
	}
}

func TestUnitDecodesSparseEnumerationCodes(t *testing.T) {
	c := load(t)

	for _, tt := range []struct {
		enumeration string
		code        int
		want        string
		found       bool
	}{
		{"Unit", 4, "years", true},
		{"Unit", 128, "seconds", true},
		{"Unit", 8192, "weeks", true},
		{"ByteCountUnit", 1, "bytes", true},
		{"ByteCountUnit", 4, "MB", true},
		{"Unit", 3, "", false},
		{"NoSuchEnumeration", 4, "", false},
	} {
		got, ok := c.Unit(tt.enumeration, tt.code)
		if ok != tt.found || got != tt.want {
			t.Errorf("Unit(%q, %d) = %q, %v; want %q, %v", tt.enumeration, tt.code, got, ok, tt.want, tt.found)
		}
	}
}

func TestFilterReturnsTypeDefaultAndUnitEnumeration(t *testing.T) {
	c := load(t)

	for _, tt := range []struct {
		property string
		want     FilterProperty
		found    bool
	}{
		{"Media Kind", FilterProperty{Type: "Enumeration", Default: "Movie"}, true},
		{"Calendar", FilterProperty{Type: "Enumeration"}, true},
		{"File Size", FilterProperty{Type: "Number", Unit: "ByteCountUnit"}, true},
		{"Duration", FilterProperty{Type: "Number", Unit: "Unit"}, true},
		{"Phone Number", FilterProperty{Type: "Phone"}, true},
		{"Nope", FilterProperty{}, false},
	} {
		got, ok := c.Filter(tt.property)
		if ok != tt.found || got != tt.want {
			t.Errorf("Filter(%q) = %+v, %v; want %+v, %v", tt.property, got, ok, tt.want, tt.found)
		}
	}
}

func TestMinimumOSVersionRoundsDownToTheNearestKnownClientVersion(t *testing.T) {
	for _, tt := range []struct {
		clientVersion int
		want          string
		found         bool
	}{
		{467, "12", true},
		{900, "13", true},
		{1106, "15", true},
		{1113, "15", true},
		{2025, "17", true},
		{3010, "18", true},
		{3216, "18.2", true},
		{5001, "27", true},
		{5023, "27", true},
		{1000, "13", true},
		{2500, "17", true},
		{3215, "18", true},
		{99999, "27", true},
		{466, "", false},
		{0, "", false},
	} {
		got, ok := MinimumOSVersion(tt.clientVersion)
		if ok != tt.found || got != tt.want {
			t.Errorf("MinimumOSVersion(%d) = %q, %v; want %q, %v", tt.clientVersion, got, ok, tt.want, tt.found)
		}
	}
}

func TestIgnoredParameters(t *testing.T) {
	for _, key := range []string{"UUID", "GroupingIdentifier", "WFControlFlowMode", "AppIntentDescriptor"} {
		if !IsIgnoredParameter(key) {
			t.Errorf("IsIgnoredParameter(%q) = false, want true", key)
		}
	}
	if IsIgnoredParameter("WFInput") {
		t.Error("IsIgnoredParameter(WFInput) = true, want false")
	}
}

func TestIgnoredParametersReturnsACopy(t *testing.T) {
	got := IgnoredParameters()
	if len(got) != 4 {
		t.Fatalf("IgnoredParameters() = %v, want 4 keys", got)
	}
	got[0] = "mutated"
	if IgnoredParameters()[0] != "UUID" {
		t.Error("IgnoredParameters() exposes its backing slice")
	}
}

func TestGroupsExposeCSSClassesAndColourNames(t *testing.T) {
	groups := Groups()
	if len(groups) != 8 {
		t.Errorf("Groups() = %v, want 8 groups", groups)
	}
	for _, group := range groups {
		if group.CSSClass() != string(group) {
			t.Errorf("%s CSSClass = %q, want %q", group, group.CSSClass(), string(group))
		}
		if group.ColorName() == "" {
			t.Errorf("%s ColorName is empty", group)
		}
	}
	if got, want := GroupDefault.CSSClass(), ""; got != want {
		t.Errorf("GroupDefault.CSSClass() = %q, want %q", got, want)
	}
	if got, want := GroupBlue.ColorName(), "Blue"; got != want {
		t.Errorf("GroupBlue.ColorName() = %q, want %q", got, want)
	}
}

func TestGroupsReturnsACopy(t *testing.T) {
	groups := Groups()
	groups[0] = "mutated"
	if Groups()[0] != GroupGray {
		t.Error("Groups() exposes its backing slice")
	}
}
