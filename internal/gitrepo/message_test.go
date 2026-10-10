package gitrepo

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommitMessages(t *testing.T) {
	tests := []struct {
		name        string
		message     CommitMessage
		wantSubject string
		wantBody    string
	}{
		{
			name:        "single shortcut",
			message:     ShortcutBackupMessage("Daily Note"),
			wantSubject: "chore(shortcuts): back up Daily Note",
		},
		{
			name:        "name with a newline is flattened into the subject",
			message:     ShortcutBackupMessage("Morning\nRoutine"),
			wantSubject: "chore(shortcuts): back up Morning Routine",
		},
		{
			name:        "name with an emoji and quotes survives",
			message:     ShortcutBackupMessage(`🚀 "Launch" Day`),
			wantSubject: `chore(shortcuts): back up 🚀 "Launch" Day`,
		},
		{
			name:        "several shortcuts are counted and listed",
			message:     ShortcutBackupMessage("Zebra", "apple", "Mango"),
			wantSubject: "chore(shortcuts): back up 3 shortcuts",
			wantBody:    "- apple\n- Mango\n- Zebra",
		},
		{
			name:        "blank names are ignored",
			message:     ShortcutBackupMessage("  ", "Daily Note", "\n"),
			wantSubject: "chore(shortcuts): back up Daily Note",
		},
		{
			name:        "no names falls back to the site message",
			message:     ShortcutBackupMessage(),
			wantSubject: "chore(site): rebuild web reviews",
		},
		{
			name:        "site rebuild",
			message:     SiteRebuildMessage(),
			wantSubject: "chore(site): rebuild web reviews",
		},
		{
			name:        "multi-line names stay readable in the body",
			message:     ShortcutBackupMessage("One\nTwo", "Alpha"),
			wantSubject: "chore(shortcuts): back up 2 shortcuts",
			wantBody:    "- Alpha\n- One\n  Two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.message.Subject != tt.wantSubject {
				t.Errorf("subject = %q, want %q", tt.message.Subject, tt.wantSubject)
			}
			if tt.message.Body != tt.wantBody {
				t.Errorf("body = %q, want %q", tt.message.Body, tt.wantBody)
			}
			if strings.Contains(tt.message.Subject, "\n") {
				t.Errorf("subject contains a newline: %q", tt.message.Subject)
			}
			if utf8.RuneCountInString(tt.message.Subject) > maxSubjectRunes {
				t.Errorf("subject is %d runes, over the %d limit", utf8.RuneCountInString(tt.message.Subject), maxSubjectRunes)
			}
		})
	}
}

func TestLongSingleShortcutNameIsTruncatedAndKeptInTheBody(t *testing.T) {
	name := strings.Repeat("Very Long Shortcut Name ", 6)
	message := ShortcutBackupMessage(name)

	if utf8.RuneCountInString(message.Subject) != maxSubjectRunes {
		t.Errorf("subject is %d runes, want %d", utf8.RuneCountInString(message.Subject), maxSubjectRunes)
	}
	if !strings.HasSuffix(message.Subject, "…") {
		t.Errorf("subject should end with an ellipsis: %q", message.Subject)
	}
	if !strings.Contains(message.Body, strings.TrimSpace(name)) {
		t.Errorf("body should carry the full name, got %q", message.Body)
	}
}

func TestCommitMessageString(t *testing.T) {
	withoutBody := CommitMessage{Subject: "chore(site): rebuild web reviews"}
	if got := withoutBody.String(); got != "chore(site): rebuild web reviews" {
		t.Errorf("String() = %q", got)
	}

	withBody := ShortcutBackupMessage("Beta", "Alpha")
	want := "chore(shortcuts): back up 2 shortcuts\n\n- Alpha\n- Beta"
	if got := withBody.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestBackupMessageListsStatusChanges(t *testing.T) {
	got := BackupMessage([]string{"Encode URL", "Daily Note"}, StatusChanges{
		Renamed:   []string{"Foo → Bar"},
		Deleted:   []string{"Old Helper"},
		Restored:  []string{"Came Back"},
		Conflicts: []string{"_Get App from Input"},
	})
	if got.Subject != "chore(shortcuts): back up 2 shortcuts" {
		t.Errorf("subject = %q", got.Subject)
	}
	want := "- Daily Note\n- Encode URL\n\n" +
		"Renamed:\n- Foo → Bar\n\n" +
		"Marked deleted (no longer in the library):\n- Old Helper\n\n" +
		"Restored (back in the library):\n- Came Back\n\n" +
		"Marked as sync-conflict copies:\n- _Get App from Input"
	if got.Body != want {
		t.Errorf("body =\n%s\nwant\n%s", got.Body, want)
	}

	if plain := BackupMessage([]string{"Daily Note"}, StatusChanges{}); plain != ShortcutBackupMessage("Daily Note") {
		t.Errorf("no status changes = %+v, want the plain backup message", plain)
	}
	if only := BackupMessage(nil, StatusChanges{Deleted: []string{"Old Helper"}}); only.Subject != "chore(shortcuts): update shortcut status" {
		t.Errorf("status-only subject = %q", only.Subject)
	}
}
