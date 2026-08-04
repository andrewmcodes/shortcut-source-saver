package render

import (
	"sort"
	"strconv"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/catalog"
)

type flowKind int

const (
	flowLeaf flowKind = iota
	flowOpen
	flowBranch
	flowClose
)

const (
	controlFlowStart  = 0
	controlFlowBranch = 1
	controlFlowEnd    = 2
)

const (
	identifierParameter = "WFWorkflowActionIdentifier"
	parametersParameter = "WFWorkflowActionParameters"
	groupingParameter   = "GroupingIdentifier"
	controlFlowMode     = "WFControlFlowMode"
	uuidParameter       = "UUID"
)

type actionInput struct {
	Index       int
	Identifier  string
	Params      map[string]any
	Mode        int
	Kind        flowKind
	Corrupted   bool
	OpenerIndex int
	ClosedAt    int
}

type outlineEntry struct {
	Index     int
	Depth     int
	Summary   string
	Corrupted bool
}

type review struct {
	Items       []*item
	Outline     []outlineEntry
	Corrupted   bool
	CorruptedAt int
	Unclosed    bool
	UnclosedAt  int
}

type reviewer struct {
	cat       *catalog.Catalog
	producers map[string]int
	consumers map[int][]int
	current   int
	expandAll bool
}

func newReviewer(actions []any, expandAll bool) *reviewer {
	r := &reviewer{
		cat:       catalog.Default(),
		producers: make(map[string]int, len(actions)),
		consumers: make(map[int][]int),
		expandAll: expandAll,
	}
	for i, raw := range actions {
		params := actionParameters(raw)
		if uuid, ok := stringAt(params, uuidParameter); ok && uuid != "" {
			if _, seen := r.producers[uuid]; !seen {
				r.producers[uuid] = i
			}
		}
	}
	return r
}

func (r *reviewer) recordConsumer(producer int) {
	if producer == r.current {
		return
	}
	r.consumers[producer] = append(r.consumers[producer], r.current)
}

func actionIdentifier(raw any) string {
	action, _ := raw.(map[string]any)
	return stringOr(action, identifierParameter)
}

func actionParameters(raw any) map[string]any {
	action, _ := raw.(map[string]any)
	params, _ := action[parametersParameter].(map[string]any)
	return params
}

type openBlock struct {
	identifier string
	grouping   string
	opener     int
	siblings   *[]*item
}

func (r *reviewer) build(actions []any) *review {
	out := &review{CorruptedAt: -1, UnclosedAt: -1}
	var roots []*item
	siblings := &roots
	var stack []openBlock
	closedAt := map[string]int{}
	byIndex := make(map[int]*item, len(actions))

	place := func(in actionInput, depth int, target *[]*item) *item {
		rendered := r.renderAction(in)
		*target = append(*target, rendered)
		byIndex[in.Index] = rendered
		out.Outline = append(out.Outline, outlineEntry{
			Index: in.Index, Depth: depth, Summary: rendered.Summary, Corrupted: in.Corrupted,
		})
		if in.Corrupted && !out.Corrupted {
			out.Corrupted = true
			out.CorruptedAt = in.Index
		}
		return rendered
	}

	for index, raw := range actions {
		identifier := actionIdentifier(raw)
		params := actionParameters(raw)
		base, catalogued := r.cat.Lookup(identifier, 0)
		control := catalogued && base.ControlFlow

		mode := controlFlowStart
		if control {
			mode, _ = intAt(params, controlFlowMode)
		}
		grouping := stringOr(params, groupingParameter)
		in := actionInput{
			Index: index, Identifier: identifier, Params: params, Mode: mode,
			OpenerIndex: -1, ClosedAt: -1,
		}

		if !control {
			place(in, len(stack), siblings)
			continue
		}

		switch mode {
		case controlFlowBranch, controlFlowEnd:
			top := len(stack) - 1
			if top < 0 || stack[top].grouping != grouping {
				in.Corrupted = true
				if at, ok := closedAt[grouping]; ok {
					in.ClosedAt = at
				}
				place(in, len(stack), siblings)
				continue
			}

			block := stack[top]
			in.OpenerIndex = block.opener
			in.Corrupted = block.identifier != identifier
			closedAt[grouping] = index

			if mode == controlFlowBranch {
				in.Kind = flowBranch
				branch := place(in, len(stack)-1, block.siblings)
				stack[top].opener = index
				siblings = &branch.Children
				continue
			}

			in.Kind = flowClose
			stack = stack[:top]
			siblings = block.siblings
			place(in, len(stack), siblings)
		default:
			in.Kind = flowOpen
			opened := place(in, len(stack), siblings)
			stack = append(stack, openBlock{
				identifier: identifier, grouping: grouping, opener: index, siblings: siblings,
			})
			siblings = &opened.Children
		}
	}

	if len(stack) > 0 {
		out.Unclosed = true
		out.UnclosedAt = stack[len(stack)-1].opener
	}

	r.linkConsumers(byIndex)
	finalize(roots, 0, r.expandAll)
	out.Items = roots
	return out
}

func (r *reviewer) linkConsumers(byIndex map[int]*item) {
	for _, producer := range sortedInts(r.consumers) {
		target, ok := byIndex[producer]
		if !ok {
			continue
		}
		target.Label = append(target.Label, text(continuationSpace+"» "))
		for i, consumer := range uniqueSorted(r.consumers[producer]) {
			if i > 0 {
				target.Label = append(target.Label, text(" "))
			}
			target.Label = append(target.Label, magic(consumer, text(strconv.Itoa(consumer))))
		}
	}
}

var depthStyles = [3]string{"", "dotted", "dashed"}

func finalize(items []*item, depth int, expandAll bool) {
	for _, current := range items {
		hasContent := len(current.Rows) > 0 || len(current.Children) > 0
		current.Container = hasContent
		current.Collapsed = hasContent && current.Big && !expandAll

		classes := make([]string, 0, 6)
		if hasContent {
			classes = append(classes, "button")
		}
		if current.Group != "" {
			classes = append(classes, current.Group)
		}
		switch current.Kind {
		case flowOpen:
			classes = append(classes, "nonbottom")
		case flowBranch:
			classes = append(classes, "nonbottom", "nontop")
		case flowClose:
			classes = append(classes, "nontop")
		}
		if depth > 0 {
			classes = append(classes, "inside")
		}
		if current.Collapsed {
			classes = append(classes, "closed")
		}
		current.ButtonClass = strings.Join(classes, " ")

		content := make([]string, 0, 5)
		content = append(content, "content")
		if current.Kind == flowOpen || current.Kind == flowBranch {
			content = append(content, "contentloop")
			if style := depthStyles[depth%len(depthStyles)]; style != "" {
				content = append(content, style)
			}
		}
		if depth > 0 {
			content = append(content, "inside")
		}
		if current.Comment {
			content = append(content, "commentcontent")
		}
		current.ContentClass = strings.Join(content, " ")

		finalize(current.Children, depth+1, expandAll)
	}
}

func sortedInts[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}

func uniqueSorted(values []int) []int {
	seen := make(map[int]bool, len(values))
	out := make([]int, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}
