package render

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	kindText    = "text"
	kindEscape  = "escape"
	kindBreak   = "break"
	kindBox     = "box"
	kindVar     = "var"
	kindMagic   = "magic"
	kindFlag    = "flag"
	kindBold    = "bold"
	kindItalic  = "italic"
	kindPage    = "page"
	kindMissing = "missing"
)

type node struct {
	Kind     string
	Text     string
	Raw      string
	Target   int
	Children []node
}

func text(s string) node { return node{Kind: kindText, Text: s} }

func textNodes(s string) []node {
	if s == "" {
		return nil
	}
	return []node{text(s)}
}

func wrap(kind string, children ...node) node {
	return node{Kind: kind, Children: children}
}

func box(children ...node) node { return wrap(kindBox, children...) }

func boxOf(children []node) node { return node{Kind: kindBox, Children: children} }

func flagged(children ...node) node { return wrap(kindFlag, children...) }

func bold(s string) node { return wrap(kindBold, text(s)) }

func italic(s string) node { return wrap(kindItalic, text(s)) }

func lineBreak() node { return node{Kind: kindBreak} }

func pageLink(href string, children ...node) node {
	return node{Kind: kindPage, Text: href, Children: children}
}

func magic(target int, children ...node) node {
	return node{Kind: kindMagic, Target: target, Children: children}
}

const (
	objectReplacement = '￼'
	continuationSpace = "　"
)

func visibleText(s string) []node {
	var out []node
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			out = append(out, text("\n"))
		}
		body, trailing := splitTrailingBlanks(line)
		out = append(out, visibleLine(body)...)
		for _, r := range trailing {
			out = append(out, node{Kind: kindEscape, Text: blankName(r), Raw: string(r)})
		}
	}
	return out
}

func splitTrailingBlanks(line string) (string, []rune) {
	end := len(line)
	for end > 0 && (line[end-1] == ' ' || line[end-1] == '\t') {
		end--
	}
	return line[:end], []rune(line[end:])
}

func blankName(r rune) string {
	if r == '\t' {
		return `\u{tab}`
	}
	return `\u{space}`
}

func visibleLine(s string) []node {
	var out []node
	var buf strings.Builder
	for _, r := range s {
		if !isInvisible(r) {
			buf.WriteRune(r)
			continue
		}
		if buf.Len() > 0 {
			out = append(out, text(buf.String()))
			buf.Reset()
		}
		out = append(out, node{Kind: kindEscape, Text: fmt.Sprintf(`\u%04X`, r), Raw: string(r)})
	}
	if buf.Len() > 0 {
		out = append(out, text(buf.String()))
	}
	return out
}

var invisibleRanges = [][2]rune{
	{0x0000, 0x0008}, {0x000B, 0x001F}, {0x007F, 0x00A0}, {0x00AD, 0x00AD},
	{0x061C, 0x061C}, {0x180E, 0x180E}, {0x2000, 0x200F}, {0x2028, 0x2029},
	{0x202A, 0x202F}, {0x205F, 0x2064}, {0x2066, 0x2069}, {0x2800, 0x2800},
	{0x3000, 0x3000}, {0xFEFF, 0xFEFF},
}

func isInvisible(r rune) bool {
	for _, span := range invisibleRanges {
		if r >= span[0] && r <= span[1] {
			return true
		}
	}
	return false
}

func plainText(nodes []node) string {
	var b strings.Builder
	writePlainText(&b, nodes)
	return b.String()
}

func writePlainText(b *strings.Builder, nodes []node) {
	for _, n := range nodes {
		switch n.Kind {
		case kindText, kindEscape:
			b.WriteString(n.Text)
		case kindBreak:
			b.WriteString("\n")
		default:
			writePlainText(b, n.Children)
		}
	}
}

func markdownText(nodes []node) string {
	var b strings.Builder
	writeMarkdownText(&b, nodes)
	return b.String()
}

func writeMarkdownText(b *strings.Builder, nodes []node) {
	for _, n := range nodes {
		switch n.Kind {
		case kindText:
			b.WriteString(n.Text)
		case kindEscape:
			b.WriteString(markdownBlank(n.Raw))
		case kindBreak:
			b.WriteString("; ")
		default:
			writeMarkdownText(b, n.Children)
		}
	}
}

func markdownBlank(raw string) string {
	for _, r := range raw {
		if unicode.IsSpace(r) {
			return " "
		}
	}
	return ""
}

func indentNodes(nodes []node, prefix string) []node {
	out := make([]node, len(nodes))
	for i, n := range nodes {
		if n.Kind == kindText {
			n.Text = strings.ReplaceAll(n.Text, "\n", "\n"+prefix)
		}
		n.Children = indentNodes(n.Children, prefix)
		out[i] = n
	}
	return out
}

func countNewlines(s string) int {
	return strings.Count(s, "\n")
}
