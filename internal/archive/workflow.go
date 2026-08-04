package archive

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

const alphaChannelBits = 8

func BuildWorkflow(w *shortcuts.Workflow) map[string]any {
	wf := map[string]any{
		"WFWorkflowActions": w.Actions,
		"WFWorkflowIcon": map[string]any{
			"WFWorkflowIconGlyphNumber": w.Icon.Glyph,
			"WFWorkflowIconStartColor":  signedIconColor(w.Icon.Color),
		},
		"WFWorkflowClientVersion":              w.ClientVersion,
		"WFWorkflowMinimumClientVersionString": w.MinimumClientVersion,
		"WFWorkflowInputContentItemClasses":    w.InputContentItemClasses,
		"WFWorkflowOutputContentItemClasses":   w.OutputContentItemClasses,
		"WFWorkflowImportQuestions":            w.ImportQuestions,
		"WFWorkflowHasOutputFallback":          w.HasOutputFallback,
		"WFWorkflowHasShortcutInputVariables":  w.HasShortcutInputVariables,
		"WFQuickActionSurfaces":                []any{},
		"WFWorkflowTypes":                      WorkflowTypes(w),
	}
	if n, err := strconv.Atoi(strings.TrimSpace(w.MinimumClientVersion)); err == nil {
		wf["WFWorkflowMinimumClientVersion"] = n
	}
	return wf
}

func WorkflowTypes(w *shortcuts.Workflow) []string {
	types := []string{}
	if len(w.InputContentItemClasses) > 0 {
		types = append(types, "ActionExtension")
	}
	if !w.HiddenFromWidget {
		types = append(types, "NCWidget")
	}
	if w.ShowInSearch {
		types = append(types, "WFWorkflowTypeShowInSearch")
	}
	return types
}

func signedIconColor(color uint32) int32 {
	return int32(color)
}

func HexColor(color uint32) string {
	return fmt.Sprintf("#%06X", (color>>alphaChannelBits)&0xFFFFFF)
}
