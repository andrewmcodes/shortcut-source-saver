package catalog

import "strings"

type Group string

const (
	GroupDefault Group = ""

	GroupGray   Group = "gGray"
	GroupGreen  Group = "gGreen"
	GroupYellow Group = "gYellow"
	GroupOrange Group = "gOrange"
	GroupRed    Group = "gRed"
	GroupBlue   Group = "gBlue"

	GroupApple   Group = "gApple"
	GroupUnknown Group = "gUnknown"
)

var groupsInDisplayOrder = []Group{
	GroupGray, GroupGreen, GroupYellow, GroupOrange, GroupRed, GroupBlue,
	GroupApple, GroupUnknown,
}

func Groups() []Group {
	return append([]Group(nil), groupsInDisplayOrder...)
}

func (g Group) CSSClass() string {
	return string(g)
}

var groupColorNames = map[Group]string{
	GroupDefault: "Default",
	GroupGray:    "Gray",
	GroupGreen:   "Green",
	GroupYellow:  "Yellow",
	GroupOrange:  "Orange",
	GroupRed:     "Red",
	GroupBlue:    "Blue",
	GroupApple:   "Apple (uncatalogued)",
	GroupUnknown: "Third party (uncatalogued)",
}

func (g Group) ColorName() string {
	if name, ok := groupColorNames[g]; ok {
		return name
	}
	return string(g)
}

func FallbackGroup(identifier string) Group {
	if IsApple(identifier) {
		return GroupApple
	}
	return GroupUnknown
}

var appleIdentifierPrefixes = []string{"is.workflow.actions.", "com.apple."}

func IsApple(identifier string) bool {
	for _, prefix := range appleIdentifierPrefixes {
		if strings.HasPrefix(identifier, prefix) {
			return true
		}
	}
	return false
}
