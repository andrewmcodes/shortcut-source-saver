package gitrepo

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxSubjectRunes = 72
	bodyIndent      = "  "
)

type CommitMessage struct {
	Subject string
	Body    string
}

func (m CommitMessage) String() string {
	if m.Body == "" {
		return m.Subject
	}
	return m.Subject + "\n\n" + m.Body
}

func SiteRebuildMessage() CommitMessage {
	return CommitMessage{Subject: "chore(site): rebuild web reviews"}
}

func ShortcutBackupMessage(names ...string) CommitMessage {
	cleaned := sortedNames(names)
	switch len(cleaned) {
	case 0:
		return SiteRebuildMessage()
	case 1:
		return singleShortcutMessage(cleaned[0])
	default:
		return CommitMessage{
			Subject: fmt.Sprintf("chore(shortcuts): back up %d shortcuts", len(cleaned)),
			Body:    bulletList(cleaned),
		}
	}
}

type StatusChanges struct {
	Deleted   []string
	Restored  []string
	Conflicts []string
}

func (c StatusChanges) empty() bool {
	return len(sortedNames(c.Deleted))+len(sortedNames(c.Restored))+len(sortedNames(c.Conflicts)) == 0
}

func BackupMessage(backedUp []string, changes StatusChanges) CommitMessage {
	if changes.empty() {
		return ShortcutBackupMessage(backedUp...)
	}
	message := ShortcutBackupMessage(backedUp...)
	if len(sortedNames(backedUp)) == 0 {
		message = CommitMessage{Subject: "chore(shortcuts): update shortcut status"}
	}

	var sections []string
	if message.Body != "" {
		sections = append(sections, message.Body)
	}
	for _, group := range []struct {
		heading string
		names   []string
	}{
		{"Marked deleted (no longer in the library):", changes.Deleted},
		{"Restored (back in the library):", changes.Restored},
		{"Marked as sync-conflict copies:", changes.Conflicts},
	} {
		if names := sortedNames(group.names); len(names) > 0 {
			sections = append(sections, group.heading+"\n"+bulletList(names))
		}
	}
	message.Body = strings.Join(sections, "\n\n")
	return message
}

func singleShortcutMessage(name string) CommitMessage {
	subject := "chore(shortcuts): back up " + singleLine(name)
	shortened := truncateRunes(subject, maxSubjectRunes)
	message := CommitMessage{Subject: shortened}
	if shortened != subject {
		message.Body = bulletList([]string{name})
	}
	return message
}

func sortedNames(names []string) []string {
	cleaned := make([]string, 0, len(names))
	for _, name := range names {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	sort.Slice(cleaned, func(i, j int) bool {
		lower, otherLower := strings.ToLower(cleaned[i]), strings.ToLower(cleaned[j])
		if lower != otherLower {
			return lower < otherLower
		}
		return cleaned[i] < cleaned[j]
	})
	return cleaned
}

func bulletList(names []string) string {
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, "- "+indentContinuations(name))
	}
	return strings.Join(lines, "\n")
}

func indentContinuations(name string) string {
	normalised := strings.ReplaceAll(strings.ReplaceAll(name, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalised, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(strings.TrimSpace(line), " \t")
	}
	kept := lines[:0]
	for _, line := range lines {
		if line != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n"+bodyIndent)
}

func singleLine(s string) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			pendingSpace = b.Len() > 0
			continue
		}
		if pendingSpace {
			b.WriteRune(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return strings.TrimRight(string(runes[:limit-1]), " ") + "…"
}
