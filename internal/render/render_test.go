package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

var update = flag.Bool("update", false, "regenerate the golden files in testdata")

type fixture struct {
	name     string
	workflow *shortcuts.Workflow
}

func action(identifier string, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	return map[string]any{
		"WFWorkflowActionIdentifier": identifier,
		"WFWorkflowActionParameters": params,
	}
}

func flowAction(identifier, grouping string, mode int, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	params["GroupingIdentifier"] = grouping
	params["WFControlFlowMode"] = uint64(mode)
	return action(identifier, params)
}

func tokenString(s string) map[string]any {
	return map[string]any{
		"WFSerializationType": "WFTextTokenString",
		"Value":               map[string]any{"string": s},
	}
}

func tokenStringWith(s string, attachments map[string]any) map[string]any {
	return map[string]any{
		"WFSerializationType": "WFTextTokenString",
		"Value": map[string]any{
			"string":             s,
			"attachmentsByRange": attachments,
		},
	}
}

func attachment(value map[string]any) map[string]any {
	return map[string]any{"WFSerializationType": "WFTextTokenAttachment", "Value": value}
}

func actionOutput(uuid, name string) map[string]any {
	return map[string]any{"Type": "ActionOutput", "OutputUUID": uuid, "OutputName": name}
}

func variableRef(name string) map[string]any {
	return map[string]any{"Type": "Variable", "VariableName": name}
}

func withAggrandizements(value map[string]any, list ...any) map[string]any {
	value["Aggrandizements"] = list
	return value
}

func dictionary(pairs ...[2]string) map[string]any {
	items := make([]any, 0, len(pairs))
	for _, pair := range pairs {
		items = append(items, map[string]any{
			"WFItemType": uint64(0),
			"WFKey":      tokenString(pair[0]),
			"WFValue":    tokenString(pair[1]),
		})
	}
	return map[string]any{
		"WFSerializationType": "WFDictionaryFieldValue",
		"Value":               map[string]any{"WFDictionaryFieldValueItems": items},
	}
}

func wrappedVariable(inner map[string]any) map[string]any {
	return map[string]any{"Type": "Variable", "Variable": inner}
}

func workflow(name string, actions ...any) *shortcuts.Workflow {
	return &shortcuts.Workflow{
		Shortcut: shortcuts.Shortcut{
			Name:        name,
			WorkflowID:  "11111111-2222-3333-4444-555555555555",
			ActionCount: len(actions),
			Created:     time.Date(2025, time.March, 1, 9, 30, 0, 0, time.UTC),
			Modified:    time.Date(2025, time.June, 14, 17, 5, 0, 0, time.UTC),
			Icon:        shortcuts.Icon{Glyph: 59511, Color: 0x4F9BFFFF},
		},
		Actions:                 actions,
		InputContentItemClasses: []any{"WFStringContentItem"},
		ClientVersion:           "3216",
		MinimumClientVersion:    "3010",
		ShowInSearch:            true,
	}
}

func fixtures() []fixture {
	return []fixture{
		{name: "linear", workflow: workflow("Linear Shortcut",
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "A",
				"WFTextActionText": tokenString("Hello, world & <friends>"),
			}),
			action("is.workflow.actions.setvariable", map[string]any{
				"UUID":           "B",
				"WFVariableName": "greeting",
				"WFInput":        attachment(actionOutput("A", "Text")),
			}),
			action("is.workflow.actions.dictionary", map[string]any{
				"UUID":    "C",
				"WFItems": dictionary([2]string{"name", "Ada"}, [2]string{"role", "engineer"}),
			}),
			action("is.workflow.actions.delay", nil),
			action("is.workflow.actions.showresult", map[string]any{
				"Text":             tokenStringWith("Greeting: ￼", map[string]any{"{10, 1}": variableRef("greeting")}),
				"CustomOutputName": "Result",
			}),
		)},

		{name: "nested-if", workflow: workflow("Nested If",
			flowAction("is.workflow.actions.conditional", "outer", 0, map[string]any{
				"UUID":                      "if1",
				"WFCondition":               uint64(4),
				"WFInput":                   attachment(variableRef("Answer")),
				"WFConditionalActionString": tokenString("yes"),
			}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "t1",
				"WFTextActionText": tokenString("inside the if"),
			}),
			flowAction("is.workflow.actions.conditional", "inner", 0, map[string]any{
				"UUID":        "if2",
				"WFCondition": uint64(1004),
				"WFConditions": map[string]any{
					"WFSerializationType": "WFContentPredicateTableTemplate",
					"Value": map[string]any{
						"WFActionParameterFilterPrefix": uint64(1),
						"WFActionParameterFilterTemplates": []any{
							map[string]any{
								"WFCondition":               uint64(99),
								"WFInput":                   attachment(actionOutput("t1", "Text")),
								"WFConditionalActionString": tokenString("the"),
							},
							map[string]any{
								"WFCondition": uint64(100),
								"WFInput":     attachment(variableRef("Answer")),
							},
						},
					},
				},
			}),
			action("is.workflow.actions.showresult", map[string]any{
				"Text": tokenString("deeply nested"),
			}),
			flowAction("is.workflow.actions.conditional", "inner", 2, map[string]any{"UUID": "end2"}),
			flowAction("is.workflow.actions.conditional", "outer", 1, map[string]any{
				"UUID":        "else1",
				"WFCondition": uint64(1003),
				"WFInput":     attachment(variableRef("Count")),
				"WFNumberValue": map[string]any{
					"WFSerializationType": "WFNumberSubstitutableState",
					"Value":               uint64(1),
				},
				"WFAnotherNumber": map[string]any{
					"WFSerializationType": "WFNumberSubstitutableState",
					"Value":               uint64(9),
				},
			}),
			action("is.workflow.actions.nothing", nil),
			flowAction("is.workflow.actions.conditional", "outer", 2, map[string]any{"UUID": "end1"}),
		)},

		{name: "repeat-menu", workflow: workflow("Repeat With Menu",
			flowAction("is.workflow.actions.repeat.count", "loop", 0, map[string]any{
				"UUID":          "r0",
				"WFRepeatCount": uint64(3),
			}),
			flowAction("is.workflow.actions.choosefrommenu", "menu", 0, map[string]any{
				"UUID":         "m0",
				"WFMenuPrompt": tokenString("Pick one"),
			}),
			flowAction("is.workflow.actions.choosefrommenu", "menu", 1, map[string]any{
				"UUID":            "m1",
				"WFMenuItemTitle": "First",
			}),
			flowAction("is.workflow.actions.conditional", "deep", 0, map[string]any{
				"UUID":        "d0",
				"WFCondition": uint64(100),
				"WFInput":     attachment(variableRef("Repeat Index")),
			}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "mt1",
				"WFTextActionText": tokenString("first branch"),
			}),
			flowAction("is.workflow.actions.conditional", "deep", 2, map[string]any{"UUID": "d1"}),
			flowAction("is.workflow.actions.choosefrommenu", "menu", 1, map[string]any{
				"UUID":                      "m2",
				"WFMenuItemAttributedTitle": tokenString("Second"),
			}),
			action("is.workflow.actions.comment", map[string]any{
				"UUID":                "c1",
				"WFCommentActionText": "a note\nover two lines",
			}),
			flowAction("is.workflow.actions.choosefrommenu", "menu", 2, map[string]any{"UUID": "m3"}),
			flowAction("is.workflow.actions.repeat.count", "loop", 2, map[string]any{"UUID": "r1"}),
		)},

		{name: "unbalanced", workflow: workflow("Unbalanced Blocks",
			flowAction("is.workflow.actions.conditional", "ghost", 2, map[string]any{"UUID": "orphan"}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "u0",
				"WFTextActionText": tokenString("after the orphan"),
			}),
			flowAction("is.workflow.actions.conditional", "mismatch", 0, map[string]any{
				"UUID":        "if0",
				"WFCondition": uint64(101),
				"WFInput":     attachment(variableRef("Thing")),
			}),
			action("is.workflow.actions.nothing", nil),
			flowAction("is.workflow.actions.repeat.count", "mismatch", 2, map[string]any{"UUID": "wrongend"}),
			flowAction("is.workflow.actions.conditional", "never-closed", 0, map[string]any{
				"UUID":        "if1",
				"WFCondition": uint64(100),
				"WFInput":     attachment(variableRef("Thing")),
			}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "u1",
				"WFTextActionText": tokenString("trapped inside"),
			}),
		)},

		{name: "magic-chain", workflow: workflow("Magic Variable Chain",
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "source",
				"WFTextActionText": tokenString("{\"total\": 3}"),
			}),
			action("is.workflow.actions.detect.dictionary", map[string]any{
				"UUID":    "dict",
				"WFInput": attachment(actionOutput("source", "Text")),
			}),
			action("is.workflow.actions.setvariable", map[string]any{
				"UUID":           "set",
				"WFVariableName": "total",
				"WFInput": attachment(withAggrandizements(actionOutput("dict", "Dictionary"),
					map[string]any{"Type": "WFDictionaryValueVariableAggrandizement", "DictionaryKey": "total"},
					map[string]any{"Type": "WFCoercionVariableAggrandizement", "CoercionItemClass": "WFNumberContentItem"},
				)),
			}),
			action("is.workflow.actions.showresult", map[string]any{
				"UUID": "show",
				"Text": tokenStringWith("total ￼ from ￼ (missing: ￼)", map[string]any{
					"{6, 1}": withAggrandizements(variableRef("total"),
						map[string]any{"Type": "WFPropertyVariableAggrandizement", "PropertyName": "Name"}),
					"{13, 1}": actionOutput("source", "Text"),
					"{25, 1}": actionOutput("nowhere", "Vanished Output"),
				}),
			}),
		)},

		{name: "token-string", workflow: workflow("Token String Splicing",
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "first",
				"WFTextActionText": tokenString("alpha"),
			}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "second",
				"WFTextActionText": tokenString("beta"),
			}),
			action("is.workflow.actions.showresult", map[string]any{
				"UUID": "join",
				"Text": tokenStringWith("ab￼cdefgh￼i", map[string]any{
					"{10, 1}": actionOutput("second", "Second Text"),
					"{2, 1}":  actionOutput("first", "First Text"),
				}),
			}),
		)},

		{name: "third-party", workflow: workflow("Third Party Actions",
			action("com.example.MyGreatApp.RunSomethingIntent", map[string]any{
				"UUID":      "tp0",
				"WFCommand": tokenString("ls -al"),
			}),
			action("com.apple.SomethingBrandNewIntent", map[string]any{
				"UUID":     "tp1",
				"WFTarget": attachment(variableRef("Thing")),
			}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "tp2",
				"WFTextActionText": tokenString("catalogued for contrast"),
			}),
		)},

		{name: "hostile-text", workflow: workflow("Hostile Text",
			action("is.workflow.actions.comment", map[string]any{
				"UUID": "h0",
				"WFCommentActionText": "<script>alert('pwned')</script> & \"quoted\" " +
					"zero\u200bwidth nbsp\u00a0here bell\u0007   \ntrailing tabs\t\t",
			}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "h1",
				"WFTextActionText": tokenString("</div></body></html>"),
			}),
			action("is.workflow.actions.setvariable", map[string]any{
				"UUID":           "h2",
				"WFVariableName": "na\u200bme",
				"WFInput":        attachment(variableRef("<b>bold</b>")),
			}),
		)},

		{name: "wrapped-variable", workflow: workflow("Wrapped Variables",
			action("is.workflow.actions.urlencode", map[string]any{
				"UUID":    "0042BBC5-732B-4EDF-AC06-4E3714215F4B",
				"WFInput": attachment(variableRef("Thing")),
			}),
			flowAction("is.workflow.actions.conditional", "E26F2EDF", 0, map[string]any{
				"UUID":        "wrapped-if",
				"WFCondition": uint64(100),
				"WFInput": wrappedVariable(attachment(
					actionOutput("0042BBC5-732B-4EDF-AC06-4E3714215F4B", "URL Encoded Text"))),
			}),
			action("is.workflow.actions.notification", map[string]any{
				"UUID": "notify",
				"WFNotificationActionBody": tokenStringWith("Input \"￼\" could not be encoded. ",
					map[string]any{"{7, 1}": wrappedVariable(attachment(map[string]any{"Type": "ExtensionInput"}))}),
			}),
			action("is.workflow.actions.setvariable", map[string]any{
				"UUID":           "deep",
				"WFVariableName": "doubly wrapped",
				"WFInput":        wrappedVariable(wrappedVariable(attachment(variableRef("Thing")))),
			}),
			flowAction("is.workflow.actions.conditional", "E26F2EDF", 2, map[string]any{"UUID": "wrapped-end"}),
		)},

		{name: "find-filter", workflow: workflow("Find Filter",
			action("is.workflow.actions.filter.photos", map[string]any{
				"UUID": "find",
				"WFContentItemFilter": predicateTable(1,
					filterTemplate("Is a Screenshot", 4, map[string]any{"Bool": true, "Unit": uint64(4)}),
					filterTemplate("Is Favorite", 5, map[string]any{"Bool": true, "Unit": uint64(4)}),
					filterTemplate("Media Type", 4, map[string]any{
						"Unit": uint64(4),
						"Enumeration": map[string]any{
							"WFSerializationType": "WFStringSubstitutableState",
							"Value":               "Image",
						},
					}),
					filterTemplate("Album", 4, map[string]any{"String": "Favorites", "Unit": uint64(4)}),
					filterTemplate("Orientation", 4, map[string]any{"Unit": uint64(4)}),
					filterTemplate("Date Taken", 1002, nil),
					filterTemplate("Creation Date", 1001, map[string]any{"Number": uint64(1), "Unit": uint64(8192)}),
					filterTemplate("Camera Make", 100, nil),
					filterTemplate("Date Taken", 101, nil),
				),
				"WFContentItemSortProperty": "Date Taken",
				"WFContentItemSortOrder":    "Latest First",
			}),
			action("is.workflow.actions.deletephotos", map[string]any{
				"photos": attachment(actionOutput("find", "Photos")),
			}),
		)},

		{name: "menu-demo", workflow: workflow("Menu Demo",
			flowAction("is.workflow.actions.choosefrommenu", "DDDD", 0, map[string]any{
				"WFMenuPrompt": "What would you like to do?",
				"WFMenuItems":  []any{"Say Hello", "Say Goodbye", tokenString("Tell a Joke")},
			}),
			flowAction("is.workflow.actions.choosefrommenu", "DDDD", 1, map[string]any{"WFMenuItemTitle": "Say Hello"}),
			action("is.workflow.actions.showresult", map[string]any{"Text": "Hello there! Nice to meet you."}),
			flowAction("is.workflow.actions.choosefrommenu", "DDDD", 1, map[string]any{"WFMenuItemTitle": "Say Goodbye"}),
			action("is.workflow.actions.showresult", map[string]any{"Text": "Goodbye! See you next time."}),
			flowAction("is.workflow.actions.choosefrommenu", "DDDD", 1, map[string]any{"WFMenuItemTitle": "Tell a Joke"}),
			action("is.workflow.actions.showresult", map[string]any{
				"Text": "Why do programmers prefer dark mode? Because light attracts bugs!",
			}),
			flowAction("is.workflow.actions.choosefrommenu", "DDDD", 2, nil),
		)},

		{name: "repeat-each", workflow: workflow("Repeat Each",
			action("is.workflow.actions.list", map[string]any{"UUID": "list"}),
			flowAction("is.workflow.actions.repeat.each", "each", 0, map[string]any{
				"UUID":    "each-start",
				"WFInput": attachment(actionOutput("list", "List")),
			}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID": "line",
				"WFTextActionText": tokenStringWith("￼ ￼ on ￼ from ￼ by ￼ (￼)", map[string]any{
					"{0, 1}":  actionOutput("each-start", "Repeat Index"),
					"{2, 1}":  actionOutput("each-start", "Repeat Item"),
					"{7, 1}":  map[string]any{"Type": "CurrentDate"},
					"{14, 1}": map[string]any{"Type": "Clipboard"},
					"{19, 1}": map[string]any{"Type": "DeviceDetails"},
					"{22, 1}": map[string]any{"Type": "Ask", "Prompt": "Who?"},
				}),
			}),
			action("is.workflow.actions.setclipboard", map[string]any{
				"WFInput": map[string]any{"Type": "CurrentDate"},
			}),
			flowAction("is.workflow.actions.repeat.each", "each", 2, map[string]any{"UUID": "each-end"}),
			action("is.workflow.actions.showresult", map[string]any{
				"Text": tokenStringWith("￼", map[string]any{"{0, 1}": actionOutput("each-end", "Repeat Results")}),
			}),
		)},

		{name: "malformed-values", workflow: workflow("Malformed Values",
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "text",
				"WFTextActionText": tokenString("value"),
			}),
			action("is.workflow.actions.conditional", map[string]any{
				"GroupingIdentifier":        "strings",
				"WFControlFlowMode":         "0",
				"WFCondition":               "Equals",
				"WFInput":                   attachment(actionOutput("text", "Text")),
				"WFConditionalActionString": "expected value",
			}),
			action("is.workflow.actions.nothing", nil),
			action("is.workflow.actions.conditional", map[string]any{
				"GroupingIdentifier": "strings",
				"WFControlFlowMode":  "2",
			}),
			flowAction("is.workflow.actions.conditional", "unknown-op", 0, map[string]any{
				"WFCondition":               uint64(42),
				"WFInput":                   attachment(actionOutput("text", "Text")),
				"WFConditionalActionString": "operand",
			}),
			flowAction("is.workflow.actions.conditional", "unknown-op", 2, nil),
			action("is.workflow.actions.conditional", map[string]any{
				"GroupingIdentifier": "junk",
				"WFControlFlowMode":  "start",
			}),
		)},

		{name: "weather-llm", workflow: workflow("Weather Report",
			action("is.workflow.actions.weather.currentconditions", map[string]any{"UUID": "EEEE"}),
			action("is.workflow.actions.gettext", map[string]any{
				"UUID": "FFFF",
				"WFTextActionText": tokenStringWith(
					"Generate a friendly weather report based on this data:\n￼\n\nKeep it brief and include clothing recommendations.",
					map[string]any{"{56, 1}": actionOutput("EEEE", "Weather Conditions")}),
			}),
			action("is.workflow.actions.askllm", map[string]any{
				"UUID":                   "GGGG",
				"WFLLMModel":             "Apple Intelligence",
				"WFGenerativeResultType": "Text",
				"WFLLMPrompt":            tokenStringWith("￼", map[string]any{"{0, 1}": actionOutput("FFFF", "Text")}),
			}),
			action("is.workflow.actions.showresult", map[string]any{
				"Text": tokenStringWith("￼", map[string]any{"{0, 1}": actionOutput("GGGG", "Response")}),
			}),
		)},

		{name: "import-questions", workflow: withDetails(workflow("Import Questions",
			action("is.workflow.actions.gettext", map[string]any{
				"UUID":             "key",
				"WFTextActionText": tokenString("replace me"),
			}),
			action("is.workflow.actions.downloadurl", map[string]any{
				"UUID":  "fetch",
				"WFURL": "https://example.com/api",
			}),
		))},
	}
}

func withDetails(w *shortcuts.Workflow) *shortcuts.Workflow {
	w.InputContentItemClasses = []any{"WFStringContentItem", "WFURLContentItem", "WFSafariWebPageContentItem", "WFAVAssetContentItem"}
	w.OutputContentItemClasses = []any{"WFStringContentItem"}
	w.HasOutputFallback = true
	w.HasShortcutInputVariables = true
	w.ImportQuestions = []any{
		map[string]any{
			"ActionIndex":  uint64(0),
			"Category":     "Parameter",
			"ParameterKey": "WFTextActionText",
			"Text":         "Paste your <API> key",
			"DefaultValue": "",
		},
		map[string]any{
			"ActionIndex":  uint64(1),
			"Category":     "Parameter",
			"ParameterKey": "WFURL",
			"Text":         "Which server?",
			"DefaultValue": "https://example.com/api",
		},
	}
	return w
}

func predicateTable(prefix int, templates ...any) map[string]any {
	return map[string]any{
		"WFSerializationType": "WFContentPredicateTableTemplate",
		"Value": map[string]any{
			"WFActionParameterFilterPrefix":    uint64(prefix),
			"WFContentPredicateBoundedDate":    false,
			"WFActionParameterFilterTemplates": templates,
		},
	}
}

func filterTemplate(property string, operator int, values map[string]any) map[string]any {
	t := map[string]any{"Property": property, "Operator": uint64(operator), "Removable": true}
	if values != nil {
		t["Values"] = values
	}
	return t
}

func TestWebReviewGoldenFiles(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			got, err := WebReview(f.workflow)
			if err != nil {
				t.Fatalf("WebReview: %v", err)
			}
			assertGolden(t, f.name+".html", got)
			assertTagsBalanced(t, got)

			again, err := WebReview(f.workflow)
			if err != nil {
				t.Fatalf("WebReview (second run): %v", err)
			}
			if !bytes.Equal(got, again) {
				t.Error("WebReview output is not byte-stable across runs")
			}
		})
	}
}

func TestShortcutREADMEGoldenFiles(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			got, err := ShortcutREADME(f.workflow)
			if err != nil {
				t.Fatalf("ShortcutREADME: %v", err)
			}
			assertGolden(t, f.name+".md", got)

			again, err := ShortcutREADME(f.workflow)
			if err != nil {
				t.Fatalf("ShortcutREADME (second run): %v", err)
			}
			if !bytes.Equal(got, again) {
				t.Error("ShortcutREADME output is not byte-stable across runs")
			}
			for _, line := range strings.Split(string(got), "\n") {
				if len(line) > 0 && strings.HasPrefix(line, "A backup of") && strings.Contains(line, "\n") {
					t.Error("prose is hard-wrapped")
				}
			}
		})
	}
}

func TestSite(t *testing.T) {
	all := fixtures()
	entries := []Entry{
		{Workflow: all[0].workflow},
		{Workflow: all[1].workflow},
		{Workflow: all[2].workflow},
	}

	site, err := Site(entries, SiteOptions{Title: "My Shortcuts", BaseURL: "/shortcut-source-saver"})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}

	want := []string{
		IndexPath,
		StylesheetPath,
		"shortcuts/linear-shortcut.html",
		"shortcuts/nested-if.html",
		"shortcuts/repeat-with-menu.html",
	}
	for _, name := range want {
		if _, ok := site[name]; !ok {
			t.Errorf("site is missing %s, got %v", name, sortedKeys(site))
		}
	}
	if len(site) != len(want) {
		t.Errorf("site has %d files, want %d: %v", len(site), len(want), sortedKeys(site))
	}

	assertGolden(t, "site-index.html", site[IndexPath])
	assertGolden(t, "site-page.html", site["shortcuts/linear-shortcut.html"])
	assertTagsBalanced(t, site[IndexPath])
	assertTagsBalanced(t, site["shortcuts/linear-shortcut.html"])

	page := string(site["shortcuts/linear-shortcut.html"])
	for _, link := range []string{
		`href="/shortcut-source-saver/assets/style.css"`,
		`href="/shortcut-source-saver/index.html"`,
	} {
		if !strings.Contains(page, link) {
			t.Errorf("shortcut page is missing %s", link)
		}
	}
	if !strings.Contains(string(site[IndexPath]), `href="/shortcut-source-saver/shortcuts/linear-shortcut.html"`) {
		t.Error("index does not link through the base URL")
	}

	again, err := Site(entries, SiteOptions{Title: "My Shortcuts", BaseURL: "/shortcut-source-saver"})
	if err != nil {
		t.Fatalf("Site (second run): %v", err)
	}
	for name, body := range site {
		if !bytes.Equal(body, again[name]) {
			t.Errorf("%s is not byte-stable across runs", name)
		}
	}
}

func TestSiteRelativeLinksAndSlugCollisions(t *testing.T) {
	first := workflow("Same Name", action("is.workflow.actions.nothing", nil))
	second := workflow("Same Name!", action("is.workflow.actions.nothing", nil))

	site, err := Site([]Entry{{Workflow: first}, {Workflow: second}}, SiteOptions{})
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	for _, name := range []string{"shortcuts/same-name.html", "shortcuts/same-name-2.html"} {
		if _, ok := site[name]; !ok {
			t.Errorf("missing %s, got %v", name, sortedKeys(site))
		}
	}
	if !strings.Contains(string(site["shortcuts/same-name.html"]), `href="../assets/style.css"`) {
		t.Error("relative sites should reach the stylesheet with ../")
	}
	if !strings.Contains(string(site[IndexPath]), `href="shortcuts/same-name.html"`) {
		t.Error("relative sites should link pages without a prefix")
	}
}

func TestEscapesShortcutText(t *testing.T) {
	got, err := WebReview(fixtureNamed(t, "hostile-text"))
	if err != nil {
		t.Fatalf("WebReview: %v", err)
	}
	page := string(got)

	if strings.Count(page, "<script") != 1 {
		t.Errorf("shortcut text leaked a script tag: %d <script occurrences", strings.Count(page, "<script"))
	}
	for _, want := range []string{
		"&lt;script&gt;alert(&#39;pwned&#39;)&lt;/script&gt;",
		`<b>\u200B</b>`,
		`<b>\u00A0</b>`,
		`<b>\u0007</b>`,
		`<b>\u{tab}</b>`,
		`<b>\u{space}</b>`,
		"&lt;/div&gt;&lt;/body&gt;&lt;/html&gt;",
		"&lt;b&gt;bold&lt;/b&gt;",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q", want)
		}
	}
	assertTagsBalanced(t, got)
}

func TestTokenStringSplicesByNumericOffset(t *testing.T) {
	got, err := WebReview(fixtureNamed(t, "token-string"))
	if err != nil {
		t.Fatalf("WebReview: %v", err)
	}
	page := string(got)
	first := strings.Index(page, "First Text")
	second := strings.Index(page, "Second Text")
	if first < 0 || second < 0 {
		t.Fatalf("attachments missing: first=%d second=%d", first, second)
	}
	if first > second {
		t.Error("attachment at offset 2 must be spliced before the one at offset 10")
	}
}

func TestMagicVariableCrossReferences(t *testing.T) {
	got, err := WebReview(fixtureNamed(t, "magic-chain"))
	if err != nil {
		t.Fatalf("WebReview: %v", err)
	}
	page := string(got)

	if !strings.Contains(page, `data-target="0"`) {
		t.Error("consumers should link back to the producing action")
	}
	if !strings.Contains(page, `<span class="flagged"><span class="var"><b>[? Vanished Output]</b>`) {
		t.Error("a dangling OutputUUID should be flagged")
	}
	consumers := `» <span class="magic" data-target="1">1</span> <span class="magic" data-target="3">3</span>`
	if !strings.Contains(page, consumers) {
		t.Error("the producer should list every action that consumes its output")
	}
}

func TestControlFlowNestsAsDOM(t *testing.T) {
	got, err := WebReview(fixtureNamed(t, "repeat-menu"))
	if err != nil {
		t.Fatalf("WebReview: %v", err)
	}
	page := string(got)
	for _, want := range []string{
		`class="content contentloop"`,
		`class="content contentloop dotted inside"`,
		`class="content contentloop dashed inside"`,
		`class="button gYellow inside" id="m4"`,
		"commentcontent",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q", want)
		}
	}
	assertTagsBalanced(t, got)
}

func TestUnbalancedControlFlowIsFlaggedNotFatal(t *testing.T) {
	got, err := WebReview(fixtureNamed(t, "unbalanced"))
	if err != nil {
		t.Fatalf("WebReview: %v", err)
	}
	page := string(got)
	if strings.Count(page, "(corrupted block)") != 2 {
		t.Errorf("want two flagged actions, got %d", strings.Count(page, "(corrupted block)"))
	}
	if !strings.Contains(page, `<span class="magic flagged"`) {
		t.Error("the header should flag the corruption")
	}
	assertTagsBalanced(t, got)
}

func TestExpandAllKeepsLargeBodiesOpen(t *testing.T) {
	big := workflow("Big Body", action("is.workflow.actions.comment", map[string]any{
		"UUID":                "big",
		"WFCommentActionText": strings.Repeat("line\n", bigBodyNewlines+5),
	}))

	collapsed, err := WebReview(big)
	if err != nil {
		t.Fatalf("WebReview: %v", err)
	}
	if !strings.Contains(string(collapsed), `style="display:none"`) {
		t.Error("a long body should be collapsed by default")
	}
	if !strings.Contains(string(collapsed), " closed") {
		t.Error("a collapsed action should carry the closed class")
	}

	expanded, err := WebReviewWithOptions(big, Options{ExpandAll: true})
	if err != nil {
		t.Fatalf("WebReviewWithOptions: %v", err)
	}
	if strings.Contains(string(expanded), `style="display:none"`) {
		t.Error("ExpandAll should leave every action open")
	}
}

func TestREADMEReviewLink(t *testing.T) {
	w := fixtureNamed(t, "linear")

	relative, err := ShortcutREADME(w)
	if err != nil {
		t.Fatalf("ShortcutREADME: %v", err)
	}
	if !strings.Contains(string(relative), "("+ReviewFilename+")") {
		t.Errorf("default README should link the sibling review, got:\n%s", relative)
	}

	hosted, err := ShortcutREADMEWithOptions(w, Options{SiteURL: "https://example.com/shortcuts-backup/"})
	if err != nil {
		t.Fatalf("ShortcutREADMEWithOptions: %v", err)
	}
	if !strings.Contains(string(hosted), "(https://example.com/shortcuts-backup/shortcuts/linear-shortcut.html)") {
		t.Errorf("hosted README should link the site page, got:\n%s", hosted)
	}
}

func TestEmptyWorkflow(t *testing.T) {
	empty := workflow("Empty")
	page, err := WebReview(empty)
	if err != nil {
		t.Fatalf("WebReview: %v", err)
	}
	assertTagsBalanced(t, page)

	readme, err := ShortcutREADME(empty)
	if err != nil {
		t.Fatalf("ShortcutREADME: %v", err)
	}
	if !strings.Contains(string(readme), "no actions") {
		t.Errorf("README should say the shortcut is empty, got:\n%s", readme)
	}
}

func TestNilWorkflow(t *testing.T) {
	if _, err := WebReview(nil); err == nil {
		t.Error("WebReview(nil) should fail")
	}
	if _, err := ShortcutREADME(nil); err == nil {
		t.Error("ShortcutREADME(nil) should fail")
	}
	if _, err := Site([]Entry{{}}, SiteOptions{}); err == nil {
		t.Error("Site with an empty entry should fail")
	}
}

func TestWrappedVariableIsUnwrapped(t *testing.T) {
	w := fixtureNamed(t, "wrapped-variable")

	page, err := WebReview(w)
	if err != nil {
		t.Fatalf("WebReview: %v", err)
	}
	for _, want := range []string{
		`<span class="magic" data-target="0"><b>[0 URL Encoded Text]</b></span>`,
		`<span class="var"><b>[Shortcut Input]</b></span>`,
		`<span class="var"><b>[Thing]</b></span>`,
	} {
		if !strings.Contains(string(page), want) {
			t.Errorf("page does not contain %q", want)
		}
	}
	if strings.Contains(string(page), "<b>[]</b>") {
		t.Error("a wrapped variable rendered as an empty reference")
	}

	readme, err := ShortcutREADME(w)
	if err != nil {
		t.Fatalf("ShortcutREADME: %v", err)
	}
	if !strings.Contains(string(readme), "`1` If [0 URL Encoded Text] has any value") {
		t.Errorf("README outline lost the wrapped variable:\n%s", readme)
	}
}

func TestMarkdownNeverShowsInvisibleEscapes(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			readme, err := ShortcutREADME(f.workflow)
			if err != nil {
				t.Fatalf("ShortcutREADME: %v", err)
			}
			if leaked := invisibleEscape.FindString(string(readme)); leaked != "" {
				t.Errorf("README leaked the HTML escape form %q", leaked)
			}
		})
	}

	readme, err := ShortcutREADME(fixtureNamed(t, "wrapped-variable"))
	if err != nil {
		t.Fatalf("ShortcutREADME: %v", err)
	}
	if !strings.Contains(string(readme), `Show Notification Input "[Shortcut Input]" could not be encoded.`+"\n") {
		t.Errorf("trailing whitespace should be trimmed, not escaped:\n%s", readme)
	}
}

func fixtureNamed(t *testing.T, name string) *shortcuts.Workflow {
	t.Helper()
	for _, f := range fixtures() {
		if f.name == name {
			return f.workflow
		}
	}
	t.Fatalf("no fixture named %q", name)
	return nil
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)

	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/render/ -update)", err)
	}
	if bytes.Equal(got, want) {
		return
	}
	t.Errorf("%s does not match the golden file (run: go test ./internal/render/ -update)\n%s",
		path, firstDifference(want, got))
}

func firstDifference(want, got []byte) string {
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var wantLine, gotLine string
		if i < len(wantLines) {
			wantLine = wantLines[i]
		}
		if i < len(gotLines) {
			gotLine = gotLines[i]
		}
		if wantLine != gotLine {
			return "line " + itoa(i+1) + ":\n  want: " + wantLine + "\n   got: " + gotLine
		}
	}
	return "files differ only in trailing bytes"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

var (
	invisibleEscape = regexp.MustCompile(`\\u(\{|[0-9A-F]{4})`)
	tagPattern      = regexp.MustCompile(`</?([a-zA-Z][a-zA-Z0-9]*)`)
	scriptPattern   = regexp.MustCompile(`(?s)<script>.*?</script>`)
	stylePattern    = regexp.MustCompile(`(?s)<style>.*?</style>`)
	voidTags        = map[string]bool{"br": true, "hr": true, "meta": true, "link": true, "img": true, "input": true}
)

func assertTagsBalanced(t *testing.T, page []byte) {
	t.Helper()
	stripped := scriptPattern.ReplaceAll(page, []byte("<script></script>"))
	stripped = stylePattern.ReplaceAll(stripped, []byte("<style></style>"))

	var stack []string
	for _, match := range tagPattern.FindAllSubmatchIndex(stripped, -1) {
		name := strings.ToLower(string(stripped[match[2]:match[3]]))
		if voidTags[name] {
			continue
		}
		if stripped[match[0]+1] == '/' {
			if len(stack) == 0 {
				t.Fatalf("closing </%s> with nothing open", name)
			}
			if top := stack[len(stack)-1]; top != name {
				t.Fatalf("closing </%s> while <%s> is open (stack %v)", name, top, stack)
			}
			stack = stack[:len(stack)-1]
			continue
		}
		stack = append(stack, name)
	}
	if len(stack) > 0 {
		t.Fatalf("unclosed tags: %v", stack)
	}
}

func TestContentClassesReadAsWords(t *testing.T) {
	got := contentClasses([]any{
		"WFSafariWebPageContentItem", "WFURLContentItem", "WFAVAssetContentItem",
		"WFiTunesProductContentItem", "WFPDFContentItem", "WFStringContentItem",
		"WFLinkEntityContentItem_com.apple.mobilenotes_AllowDarkBackgroundsEntity",
		"WFLinkEntityContentItem_com.apple.reminders_RemindersSettingsDeepLinks",
	})
	want := "Safari Web Page, URL, AV Asset, iTunes Product, PDF, Text, Allow Dark Backgrounds, Reminders Settings Deep Links"
	if got != want {
		t.Errorf("contentClasses() = %q, want %q", got, want)
	}
}
