package catalog

import "testing"

func TestDisplayNameFallsBackToTheIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		want       string
	}{
		{"legacy action", "is.workflow.actions.getvariable", "Get Variable"},
		{"legacy dotted action", "is.workflow.actions.text.replace", "Text Replace"},
		{"legacy multi word action", "is.workflow.actions.repeat.count", "Repeat Count"},
		{"hyphen inside a segment is preserved", "is.workflow.actions.display.always-on.set", "Display Always-On Set"},
		{"acronym as a whole segment", "is.workflow.actions.url", "URL"},
		{"acronym inside a lowercase run", "is.workflow.actions.urlencode", "URL Encode"},
		{"apple intent", "com.apple.Health.OpenDataTypeIntent", "Health Open Data Type Intent"},
		{"apple intent with a lowercase bundle segment", "com.apple.mobiletimer.CreateAlarmIntent", "Mobile Timer Create Alarm Intent"},
		{"third party keeps the app and intent verbatim", "AsheKube.app.a-Shell-mini.ExecuteCommandIntent", "a-Shell-mini > ExecuteCommandIntent"},
		{"third party consumes one prefix component at a time", "com.example.dev.MyApp.Nested.DoThing", "dev > MyApp.Nested.DoThing"},
		{"third party too shallow to split into app and intent", "MyApp.DoThing", "My App Do Thing"},
		{"bare identifier", "gettext", "Get Text"},
		{"lowercase run no vocabulary covers stays intact", "is.workflow.actions.qqzzxx", "Qqzzxx"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DisplayName(tt.identifier); got != tt.want {
				t.Errorf("DisplayName(%q) = %q, want %q", tt.identifier, got, tt.want)
			}
		})
	}
}

func TestIsApple(t *testing.T) {
	for _, tt := range []struct {
		identifier string
		want       bool
	}{
		{"is.workflow.actions.comment", true},
		{"com.apple.Health.OpenDataTypeIntent", true},
		{"AsheKube.app.a-Shell-mini.ExecuteCommandIntent", false},
		{"com.appleseed.NotApple", false},
		{"", false},
	} {
		if got := IsApple(tt.identifier); got != tt.want {
			t.Errorf("IsApple(%q) = %v, want %v", tt.identifier, got, tt.want)
		}
	}
}

func TestFallbackGroupSeparatesAppleFromThirdParty(t *testing.T) {
	for _, tt := range []struct {
		identifier string
		want       Group
	}{
		{"com.apple.SomeNewIntent", GroupApple},
		{"is.workflow.actions.somethingnew", GroupApple},
		{"AsheKube.app.a-Shell-mini.ExecuteCommandIntent", GroupUnknown},
	} {
		if got := FallbackGroup(tt.identifier); got != tt.want {
			t.Errorf("FallbackGroup(%q) = %q, want %q", tt.identifier, got, tt.want)
		}
	}
}

func TestSplitVocabularyPrefersTheFewestWordsAndGivesUpCleanly(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want []string
	}{
		{"getvariable", []string{"get", "variable"}},
		{"setvolume", []string{"set", "volume"}},
		{"gettext", []string{"get", "text"}},
		{"framework", []string{"framework"}},
		{"nothingmatcheshere", nil},
	} {
		got := splitVocabulary(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("splitVocabulary(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitVocabulary(%q) = %v, want %v", tt.in, got, tt.want)
				break
			}
		}
	}
}
