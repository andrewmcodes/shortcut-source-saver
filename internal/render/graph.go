package render

import (
	"html/template"
	"sort"
	"strconv"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
)

const (
	mapNodeWidth   = 210
	mapNodeHeight  = 62
	mapColumnGap   = 96
	mapRowHeight   = 88
	mapPadding     = 20
	mapTop         = 44
	mapVirtualSlot = 20
	mapNameLimit   = 24
	mapSweeps      = 8
)

const (
	nodeShortcut = "shortcut"
	nodeMissing  = "missing"
	nodeDynamic  = "dynamic"
	nodeVirtual  = "virtual"
)

type mapNode struct {
	Key      string
	Anchor   string
	Name     string
	Short    string
	Subtitle string
	Label    string
	Href     string
	Kind     string
	Color    string
	DarkIcon bool
	X, Y     int
	DotX     int
	DotY     int
	TextX    int
	NameY    int
	SubY     int
	W, H     int

	entry int
	order int
	rank  int
	cycle bool
	sort  string
}

type mapEdge struct {
	From  string
	To    string
	Kind  string
	Title string
	Path  string

	from, to int
	count    int
	via      []int
}

type mapColumn struct {
	X     int
	Label string
}

type mapGroup struct {
	Members string
	Label   string
	Width   int
	Height  int
	Columns []mapColumn
	Edges   []mapEdge
	Nodes   []mapNode
}

type mapOption struct {
	Value string
	Name  string
}

type mapRow struct {
	Name     string
	Href     string
	Color    template.CSS
	DarkIcon bool
	Related  string
	Runs     []relation
	Callers  int
}

type mapPage struct {
	Title      string
	Stylesheet string
	Chrome     chrome
	Home       string
	Connected  int
	Standalone int
	Links      int
	Missing    int
	Dynamic    int
	Groups     []mapGroup
	Options    []mapOption
	Rows       []mapRow
}

func (g *siteGraph) mapPage(href func(slug string) string) mapPage {
	page := mapPage{}
	var nodes []*mapNode
	byEntry := map[int]int{}
	placeholders := map[string]int{}

	for i := range g.entries {
		if !g.connected(i) {
			page.Standalone++
			continue
		}
		page.Connected++
		w := g.entries[i].Workflow
		color := archive.HexColor(w.Icon.Color)
		byEntry[i] = len(nodes)
		nodes = append(nodes, &mapNode{
			Key: g.slugs[i], Anchor: mapAnchor(g.slugs[i]), Name: w.Name, Href: href(g.slugs[i]),
			Kind: nodeShortcut, Color: color, DarkIcon: isDarkColor(color),
			Subtitle: plural(len(w.Actions), "action"), entry: i,
			sort: strings.ToLower(w.Name) + "\x00" + g.slugs[i],
		})
	}

	edgeAt := map[[2]int]int{}
	var edges []*mapEdge
	addEdge := func(from, to int, kind string) {
		key := [2]int{from, to}
		if at, ok := edgeAt[key]; ok {
			edges[at].count++
			return
		}
		edgeAt[key] = len(edges)
		edges = append(edges, &mapEdge{from: from, to: to, Kind: kind, count: 1})
	}
	placeholder := func(key, kind, name, subtitle string) int {
		if at, ok := placeholders[key]; ok {
			return at
		}
		at := len(nodes)
		placeholders[key] = at
		nodes = append(nodes, &mapNode{
			Name: name, Kind: kind, Subtitle: subtitle, entry: -1,
			sort: "\xff" + strings.ToLower(name) + "\x00" + key,
		})
		return at
	}

	for i, calls := range g.calls {
		from, ok := byEntry[i]
		if !ok {
			continue
		}
		for _, c := range calls {
			switch c.Status {
			case callLinked, callByName, callSelf:
				addEdge(from, byEntry[c.Target], "")
				if c.Status != callSelf {
					page.Links++
				}
			case callDynamic:
				page.Dynamic++
				addEdge(from, placeholder("dynamic:"+g.slugs[i], nodeDynamic, "Chosen at run time", "From a variable"), nodeDynamic)
			default:
				name := c.Name
				if name == "" {
					name = "No shortcut chosen"
				}
				addEdge(from, placeholder(relationKey(c), nodeMissing, name, "Missing shortcut"), nodeMissing)
			}
		}
	}

	var pending []*mapNode
	for _, n := range nodes {
		if n.Kind != nodeShortcut {
			pending = append(pending, n)
		}
	}
	sort.SliceStable(pending, func(a, b int) bool { return pending[a].sort < pending[b].sort })
	for i, n := range pending {
		n.Key = n.Kind + "-" + strconv.Itoa(i+1)
	}
	sort.SliceStable(edges, func(a, b int) bool {
		if edges[a].from != edges[b].from {
			return nodes[edges[a].from].sort < nodes[edges[b].from].sort
		}
		return nodes[edges[a].to].sort < nodes[edges[b].to].sort
	})
	for _, n := range nodes {
		n.Short = truncate(n.Name, mapNameLimit)
		n.Label = n.Name + ", " + n.Subtitle
	}
	for _, e := range edges {
		e.From, e.To = nodes[e.from].Key, nodes[e.to].Key
		e.Title = edgeTitle(nodes[e.from], nodes[e.to], e)
	}

	for _, component := range components(nodes, edges) {
		page.Groups = append(page.Groups, layoutGroup(nodes, edges, component))
	}

	for i := range g.entries {
		page.Missing += g.missing(i)
	}
	for i := range g.entries {
		if !g.connected(i) {
			continue
		}
		page.Options = append(page.Options, mapOption{Value: g.slugs[i], Name: g.entries[i].Workflow.Name})
		page.Rows = append(page.Rows, g.mapRow(i, href))
	}
	sort.SliceStable(page.Options, func(a, b int) bool {
		return lessName(page.Options[a].Name, page.Options[a].Value, page.Options[b].Name, page.Options[b].Value)
	})
	sort.SliceStable(page.Rows, func(a, b int) bool {
		return lessName(page.Rows[a].Name, page.Rows[a].Href, page.Rows[b].Name, page.Rows[b].Href)
	})
	return page
}

func (g *siteGraph) mapRow(i int, href func(slug string) string) mapRow {
	deps := g.pageDeps(i, href, "")
	row := g.shortcutRelation(i, href, "", "")
	related := []string{g.slugs[i]}
	for _, c := range g.calls[i] {
		if c.resolved() {
			related = append(related, g.slugs[c.Target])
		}
	}
	for _, caller := range g.callers[i] {
		related = append(related, g.slugs[caller])
	}
	callers := 0
	for _, caller := range g.callers[i] {
		if caller != i {
			callers++
		}
	}
	return mapRow{
		Name: row.Name, Href: row.Href, Color: row.Color, DarkIcon: row.DarkIcon,
		Related: strings.Join(uniqueStrings(related), " "), Runs: deps.Runs, Callers: callers,
	}
}

func edgeTitle(from, to *mapNode, e *mapEdge) string {
	var title string
	switch e.Kind {
	case nodeMissing:
		title = from.Name + " runs " + to.Name + ", which is not in this backup"
	case nodeDynamic:
		title = from.Name + " runs a shortcut chosen at run time"
	default:
		title = from.Name + " runs " + to.Name
	}
	if e.count > 1 {
		title += " · " + strconv.Itoa(e.count) + " calls"
	}
	return title
}

func components(nodes []*mapNode, edges []*mapEdge) [][]int {
	parent := make([]int, len(nodes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for _, e := range edges {
		a, b := find(e.from), find(e.to)
		if a != b {
			parent[max(a, b)] = min(a, b)
		}
	}
	groups := map[int][]int{}
	for i := range nodes {
		root := find(i)
		groups[root] = append(groups[root], i)
	}
	out := make([][]int, 0, len(groups))
	for _, members := range groups {
		sort.Slice(members, func(a, b int) bool { return nodes[members[a]].sort < nodes[members[b]].sort })
		out = append(out, members)
	}
	sort.Slice(out, func(a, b int) bool {
		if len(out[a]) != len(out[b]) {
			return len(out[a]) > len(out[b])
		}
		return nodes[out[a][0]].sort < nodes[out[b][0]].sort
	})
	return out
}

func layoutGroup(all []*mapNode, allEdges []*mapEdge, members []int) mapGroup {
	local := make(map[int]int, len(members))
	for i, n := range members {
		local[n] = i
	}
	var edges []*mapEdge
	for _, e := range allEdges {
		if _, ok := local[e.from]; ok {
			edges = append(edges, e)
		}
	}
	adjacent := make([][]int, len(members))
	for _, e := range edges {
		from, to := local[e.from], local[e.to]
		adjacent[from] = append(adjacent[from], to)
		if from == to {
			all[e.from].cycle = true
		}
	}

	componentOf := stronglyConnected(adjacent)
	count := 0
	for _, c := range componentOf {
		count = max(count, c+1)
	}
	size := make([]int, count)
	for _, c := range componentOf {
		size[c]++
	}
	successors := make([]map[int]bool, count)
	indegree := make([]int, count)
	for i := range successors {
		successors[i] = map[int]bool{}
	}
	for from, targets := range adjacent {
		for _, to := range targets {
			a, b := componentOf[from], componentOf[to]
			if a != b && !successors[a][b] {
				successors[a][b] = true
				indegree[b]++
			}
		}
	}
	var order []int
	for c := 0; c < count; c++ {
		if indegree[c] == 0 {
			order = append(order, c)
		}
	}
	for next := 0; next < len(order); next++ {
		for _, b := range sortedInts(successors[order[next]]) {
			if indegree[b]--; indegree[b] == 0 {
				order = append(order, b)
			}
		}
	}
	height := make([]int, count)
	tallest := 0
	for i := len(order) - 1; i >= 0; i-- {
		for b := range successors[order[i]] {
			height[order[i]] = max(height[order[i]], height[b]+1)
		}
		tallest = max(tallest, height[order[i]])
	}
	rank := make([]int, count)
	for c := range rank {
		rank[c] = tallest - height[c]
	}

	columnCount := 0
	for i, n := range members {
		node := all[n]
		node.rank = rank[componentOf[i]]
		if size[componentOf[i]] > 1 {
			node.cycle = true
		}
		columnCount = max(columnCount, node.rank+1)
	}

	nodes := append([]*mapNode(nil), all...)
	columns := make([][]int, columnCount)
	for _, n := range members {
		columns[nodes[n].rank] = append(columns[nodes[n].rank], n)
	}
	var segments [][2]int
	for _, e := range edges {
		e.via = nil
		from, to := nodes[e.from], nodes[e.to]
		if to.rank <= from.rank {
			continue
		}
		previous := e.from
		for r := from.rank + 1; r < to.rank; r++ {
			virtual := len(nodes)
			nodes = append(nodes, &mapNode{Kind: nodeVirtual, rank: r, entry: -1})
			columns[r] = append(columns[r], virtual)
			e.via = append(e.via, virtual)
			segments = append(segments, [2]int{previous, virtual})
			previous = virtual
		}
		segments = append(segments, [2]int{previous, e.to})
	}
	orderColumns(nodes, segments, columns)

	tallest = 0
	heights := make([]int, columnCount)
	for c, column := range columns {
		for _, n := range column {
			heights[c] += slotHeight(nodes[n])
		}
		tallest = max(tallest, heights[c])
	}
	for c, column := range columns {
		y := mapTop + (tallest-heights[c])/2
		for _, n := range column {
			node := nodes[n]
			node.X = mapPadding + c*(mapNodeWidth+mapColumnGap)
			node.Y = y
			y += slotHeight(node)
			if node.Kind == nodeVirtual {
				node.DotY = node.Y + mapVirtualSlot/2
				continue
			}
			node.W, node.H = mapNodeWidth, mapNodeHeight
			node.DotX, node.DotY = node.X+22, node.Y+mapNodeHeight/2
			node.TextX, node.NameY, node.SubY = node.X+38, node.Y+27, node.Y+46
			if node.cycle && node.Kind == nodeShortcut {
				node.Subtitle += " · recursive"
				node.Label = node.Name + ", " + node.Subtitle
			}
		}
	}

	loopRoom := 0
	for _, e := range edges {
		if e.from != e.to && nodes[e.to].rank <= nodes[e.from].rank && nodes[e.from].rank == columnCount-1 {
			loopRoom = 44
		}
	}
	group := mapGroup{
		Width:  max(2*mapPadding+columnCount*mapNodeWidth+(columnCount-1)*mapColumnGap+loopRoom, 300),
		Height: mapTop + tallest - (mapRowHeight - mapNodeHeight) + 24,
	}
	for c := range columns {
		group.Columns = append(group.Columns, mapColumn{X: mapPadding + 2 + c*(mapNodeWidth+mapColumnGap), Label: columnLabel(c, columnCount)})
	}
	routeEdges(nodes, edges)
	var names []string
	var keys []string
	for _, column := range columns {
		for _, n := range column {
			if nodes[n].Kind == nodeVirtual {
				continue
			}
			group.Nodes = append(group.Nodes, *nodes[n])
			if nodes[n].Kind == nodeShortcut {
				keys = append(keys, nodes[n].Key)
				names = append(names, nodes[n].Name)
			}
		}
	}
	sort.SliceStable(edges, func(a, b int) bool {
		if edges[a].from != edges[b].from {
			return nodes[edges[a].from].sort < nodes[edges[b].from].sort
		}
		return nodes[edges[a].to].sort < nodes[edges[b].to].sort
	})
	for _, e := range edges {
		group.Edges = append(group.Edges, *e)
	}
	sort.Strings(keys)
	group.Members = strings.Join(keys, " ")
	group.Label = groupLabel(names)
	return group
}

func slotHeight(n *mapNode) int {
	if n.Kind == nodeVirtual {
		return mapVirtualSlot
	}
	return mapRowHeight
}

func groupLabel(names []string) string {
	sorted := append([]string(nil), names...)
	sort.SliceStable(sorted, func(a, b int) bool { return strings.ToLower(sorted[a]) < strings.ToLower(sorted[b]) })
	switch len(sorted) {
	case 0:
		return "Shortcut dependencies"
	case 1:
		return "Dependencies of " + sorted[0]
	case 2:
		return "Dependencies between " + sorted[0] + " and " + sorted[1]
	default:
		return "Dependencies between " + sorted[0] + " and " + plural(len(sorted)-1, "other shortcut")
	}
}

func columnLabel(c, count int) string {
	switch {
	case count == 1:
		return "Shortcuts"
	case c == 0:
		return "Callers"
	case c == count-1:
		return "Called shortcuts"
	default:
		return "Building blocks"
	}
}

func stronglyConnected(adjacent [][]int) []int {
	index := 0
	indices := make([]int, len(adjacent))
	low := make([]int, len(adjacent))
	onStack := make([]bool, len(adjacent))
	componentOf := make([]int, len(adjacent))
	for i := range indices {
		indices[i] = -1
	}
	var stack []int
	components := 0
	var visit func(int)
	visit = func(v int) {
		indices[v], low[v] = index, index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adjacent[v] {
			if indices[w] < 0 {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], indices[w])
			}
		}
		if low[v] == indices[v] {
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				componentOf[w] = components
				if w == v {
					break
				}
			}
			components++
		}
	}
	for v := range adjacent {
		if indices[v] < 0 {
			visit(v)
		}
	}
	return componentOf
}

func orderColumns(nodes []*mapNode, segments [][2]int, columns [][]int) {
	predecessors := map[int][]int{}
	successors := map[int][]int{}
	for _, segment := range segments {
		successors[segment[0]] = append(successors[segment[0]], segment[1])
		predecessors[segment[1]] = append(predecessors[segment[1]], segment[0])
	}
	for _, column := range columns {
		for r, n := range column {
			nodes[n].order = r
		}
	}
	relative := func(n int) float64 {
		return (float64(nodes[n].order) + 0.5) / float64(len(columns[nodes[n].rank]))
	}
	reorder := func(column []int, neighbours map[int][]int) {
		score := make(map[int]float64, len(column))
		for _, n := range column {
			linked := neighbours[n]
			if len(linked) == 0 {
				score[n] = relative(n)
				continue
			}
			total := 0.0
			for _, m := range linked {
				total += relative(m)
			}
			score[n] = total / float64(len(linked))
		}
		sort.SliceStable(column, func(a, b int) bool {
			if score[column[a]] != score[column[b]] {
				return score[column[a]] < score[column[b]]
			}
			return nodes[column[a]].order < nodes[column[b]].order
		})
		for r, n := range column {
			nodes[n].order = r
		}
	}
	for pass := 0; pass < mapSweeps; pass++ {
		if pass%2 == 0 {
			for c := 1; c < len(columns); c++ {
				reorder(columns[c], predecessors)
			}
			continue
		}
		for c := len(columns) - 2; c >= 0; c-- {
			reorder(columns[c], successors)
		}
	}
}

func routeEdges(nodes []*mapNode, edges []*mapEdge) {
	centre := func(n int) int {
		if nodes[n].Kind == nodeVirtual {
			return nodes[n].DotY
		}
		return nodes[n].Y + mapNodeHeight/2
	}
	next := func(e *mapEdge) int {
		if len(e.via) > 0 {
			return centre(e.via[0])
		}
		return centre(e.to)
	}
	previous := func(e *mapEdge) int {
		if len(e.via) > 0 {
			return centre(e.via[len(e.via)-1])
		}
		return centre(e.from)
	}
	sort.SliceStable(edges, func(a, b int) bool {
		ea, eb := edges[a], edges[b]
		if nodes[ea.from].Y != nodes[eb.from].Y {
			return nodes[ea.from].Y < nodes[eb.from].Y
		}
		return next(ea) < next(eb)
	})
	outgoing := map[int][]*mapEdge{}
	incoming := map[int][]*mapEdge{}
	for _, e := range edges {
		outgoing[e.from] = append(outgoing[e.from], e)
		incoming[e.to] = append(incoming[e.to], e)
	}
	for _, list := range outgoing {
		sort.SliceStable(list, func(a, b int) bool { return next(list[a]) < next(list[b]) })
	}
	for _, list := range incoming {
		sort.SliceStable(list, func(a, b int) bool { return previous(list[a]) < previous(list[b]) })
	}
	position := func(list []*mapEdge, e *mapEdge) int {
		for i, candidate := range list {
			if candidate == e {
				return i
			}
		}
		return 0
	}
	lanes := map[int]int{}
	loops := 0
	for _, e := range edges {
		p, q := nodes[e.from], nodes[e.to]
		source, target := outgoing[e.from], incoming[e.to]
		sy := p.Y + mapNodeHeight*(position(source, e)+1)/(len(source)+1)
		ty := q.Y + mapNodeHeight*(position(target, e)+1)/(len(target)+1)
		if e.from == e.to {
			bottom := p.Y + mapNodeHeight
			e.Path = pathData([][2]int{{p.X + mapNodeWidth - 60, bottom}, {p.X + mapNodeWidth - 60, bottom + 14}, {p.X + mapNodeWidth - 24, bottom + 14}, {p.X + mapNodeWidth - 24, bottom}})
			continue
		}
		if q.rank <= p.rank {
			x := p.X + mapNodeWidth + 18 + (loops%4)*8
			below := q.Y + mapNodeHeight + 12 + (loops%3)*4
			loops++
			e.Path = pathData([][2]int{{p.X + mapNodeWidth, sy}, {x, sy}, {x, below}, {q.X + mapNodeWidth - 24, below}, {q.X + mapNodeWidth - 24, q.Y + mapNodeHeight}})
			continue
		}
		points := [][2]int{{p.X + mapNodeWidth, sy}}
		y, rank := sy, p.rank
		stations := append(append([]int(nil), e.via...), e.to)
		for _, station := range stations {
			lane := lanes[rank]
			lanes[rank]++
			mid := mapPadding + rank*(mapNodeWidth+mapColumnGap) + mapNodeWidth + 30 + (lane%8)*8
			ny := ty
			if station != e.to {
				ny = centre(station)
			}
			if ny == y {
				points = append(points, [2]int{nodes[station].X, ny})
			} else {
				points = append(points, [2]int{mid, y}, [2]int{mid, ny}, [2]int{nodes[station].X, ny})
			}
			if station != e.to {
				points = append(points, [2]int{nodes[station].X + mapNodeWidth, ny})
			}
			y, rank = ny, nodes[station].rank
		}
		e.Path = pathData(points)
	}
}

func pathData(points [][2]int) string {
	var b strings.Builder
	for i, p := range points {
		if i == 0 {
			b.WriteString("M")
		} else {
			b.WriteString(" L")
		}
		b.WriteString(strconv.Itoa(p[0]))
		b.WriteString(" ")
		b.WriteString(strconv.Itoa(p[1]))
	}
	return b.String()
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func lessName(leftName, leftKey, rightName, rightKey string) bool {
	left, right := strings.ToLower(leftName), strings.ToLower(rightName)
	if left != right {
		return left < right
	}
	return leftKey < rightKey
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
