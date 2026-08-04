package backup

import (
	"testing"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

func shortcut(name, id string) shortcuts.Shortcut {
	return shortcuts.Shortcut{Name: name, WorkflowID: id}
}

func TestResolveSlugsGivesCollidingNamesDistinctDirectories(t *testing.T) {
	all := []shortcuts.Shortcut{
		shortcut("Shortcut Source Tool", "540BFD88-7DDE-4C5F-A838-383188E19547"),
		shortcut("Shortcut - Source Tool", "1FCAD5AA-5207-4338-BB10-AAE36C803657"),
		shortcut("Summarize PDF", "AAAAAAAA-0000-0000-0000-000000000001"),
		shortcut("Summarize PDF", "BBBBBBBB-0000-0000-0000-000000000002"),
		shortcut("Encode URL", "44434252-BEA5-4DA6-BC72-949BD054B2E3"),
	}

	slugs := ResolveSlugs(all)

	if len(slugs) != len(all) {
		t.Fatalf("resolved %d slugs for %d shortcuts", len(slugs), len(all))
	}

	seen := map[string]string{}
	for _, sc := range all {
		slug := slugs[sc.WorkflowID]
		if slug == "" {
			t.Errorf("%q got an empty slug", sc.Name)
			continue
		}
		if other, clash := seen[slug]; clash {
			t.Errorf("slug %q used by both %q and %q", slug, other, sc.WorkflowID)
		}
		seen[slug] = sc.WorkflowID
	}

	if got := slugs["44434252-BEA5-4DA6-BC72-949BD054B2E3"]; got != "encode-url" {
		t.Errorf("unique name got slug %q, want the plain slug %q", got, "encode-url")
	}
}

func TestResolveSlugsIsStableAcrossCalls(t *testing.T) {
	all := []shortcuts.Shortcut{
		shortcut("Summarize PDF", "AAAAAAAA-0000-0000-0000-000000000001"),
		shortcut("Summarize PDF", "BBBBBBBB-0000-0000-0000-000000000002"),
		shortcut("Encode URL", "44434252-BEA5-4DA6-BC72-949BD054B2E3"),
	}

	first := ResolveSlugs(all)
	reordered := []shortcuts.Shortcut{all[2], all[1], all[0]}
	second := ResolveSlugs(reordered)

	for id, slug := range first {
		if second[id] != slug {
			t.Errorf("%s: slug changed from %q to %q when the input order changed", id, slug, second[id])
		}
	}
}

func TestResolveSlugsHandlesNamesThatSlugToNothing(t *testing.T) {
	all := []shortcuts.Shortcut{
		shortcut("💥💥", "AAAAAAAA-0000-0000-0000-000000000001"),
		shortcut("🎉", "BBBBBBBB-0000-0000-0000-000000000002"),
	}

	slugs := ResolveSlugs(all)

	seen := map[string]bool{}
	for _, sc := range all {
		slug := slugs[sc.WorkflowID]
		if slug == "" {
			t.Fatalf("%q got an empty slug", sc.Name)
		}
		if seen[slug] {
			t.Errorf("emoji-only names collided on slug %q", slug)
		}
		seen[slug] = true
	}
}

func TestSignedFileNameKeepsTheDisplayNameAndStripsPathSeparators(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Encode URL", "Encode URL.shortcut"},
		{"Move to Archive", "Move to Archive.shortcut"},
		{"a/b", "a-b.shortcut"},
		{"", "shortcut.shortcut"},
		{"💥💥Add book", "💥💥Add book.shortcut"},
	}
	for _, tt := range tests {
		if got := signedFileName(tt.name); got != tt.want {
			t.Errorf("signedFileName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestParseHexColorRoundTripsTheArchiveFormat(t *testing.T) {
	tests := []struct {
		in   string
		want uint32
	}{
		{"#FFD426", 0xFFD426FF},
		{"#ff4351", 0xFF4351FF},
		{"", 0},
		{"#12345", 0},
		{"#GGGGGG", 0},
	}
	for _, tt := range tests {
		if got := parseHexColor(tt.in); got != tt.want {
			t.Errorf("parseHexColor(%q) = %#08x, want %#08x", tt.in, got, tt.want)
		}
	}
}
