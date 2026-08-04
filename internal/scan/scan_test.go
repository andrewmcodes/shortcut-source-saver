package scan

import (
	"strings"
	"testing"
)

const (
	openAIKey     = "sk-u8jzPde0IgxLd6GncfBAepfJBd0Kh8oOOL8dKLzdocJ2isAj"
	openAIProjKey = "sk-proj-IhKtJ0RlgLKOmxgJTeKdNnFRIBXuDL7DxtpYlSXpfKtHF4vU"
	anthropicKey  = "sk-ant-api03-CsMehGAkWvj7FAc9QeWJKY40uvSwMFLZDe1f8rES"
	githubPAT     = "ghp_QedUStPKR0CsTy4Qwb8DwkNhFdnXsiVpzz63"
	githubOAuth   = "gho_FfkCzJr4i0B3JrTAwR4y9ojfljoQoaF1Llqs"
	githubFinePAT = "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"
	awsKeyID      = "AKIAVQ4K7BN7XJ8B7TFQ"
	awsTempKeyID  = "ASIA7XKWO886VOMPZOM7"
	googleKey     = "AIza_TddJ8HyS5SUkCnD8zRA9a9SkpXz9w3QlY7"
	slackToken    = "xoxb-fixture-notarealslacktoken-QWJjRGVmR2hpSmts"
	bearerToken   = "DQL05HA064GiIjHGb3CXlMaXZjljENUhJduRHHJE"
	basicCreds    = "YXg4JdpmrcXgGCJbW56eCuNG=="
	genericSecret = "MGmSrCGIZEG8pSH4487q7J58m1CiAhzC"
	headerSecret  = "ueQpBenQtYh5Xj8TPQxjq4i9DoV8gz4FkQ1o"
	jwt           = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiI5MDIxIiwibmFtZSI6IkFuZHJldyIsImlhdCI6MTcxMDAwMDAwMH0.KcRt7WgHqLm3vZpBn8sYdT4XeQfJi2UoNbHlAzGxPwM"
	pemKey        = "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAwJ3kQ8vLmNpRtYuIoAsDfGhJkLzXcVbNmQwErTyUiOpAsDfGh\n-----END RSA PRIVATE KEY-----"
)

const base64Image = "u3ZR1zTOlUcR64cXQLioDnkHIfxIq2HZt/PlJhx2jIclHkCiHp6bR1IqfEouHgxzNNAL5wIScGebcy8Fv9nBu5EGKO6uZgYAY0kPYbXK1DkrJoNCE8ojoJm5wSHZ4ZxpTFNjqZO0KO1Bq5jVOaKMchcnq8gELUKfPTAbaGjxAcHDlIcJPFbHUuoNKQZeeQMZs4GApNJVLIvxlUKFEDRvCLQZTPtnhoRWvbNbHDoyKMHzKZKb3uWMEQqTOKKQzT9OQKAcNIQvbjfKGxTPqBOmzOtNRAlLKrVYuQlZBTFWKfEXAoLmMEUZjMLKQTHNZBAqOJEBZQNKQGRPvDMoZWtHqTKZJERPKQCLOBZVMxNQZTdKRLPvQFAxTGZE"

func textAction(value string) map[string]any {
	return map[string]any{
		ActionIdentifierKey: "is.workflow.actions.gettext",
		ActionParametersKey: map[string]any{
			"WFTextActionText": value,
			"UUID":             "6C6E4C40-8A9F-4E67-9D9A-3E1D8C58F1A2",
		},
	}
}

func urlAction(headerName, headerValue string) map[string]any {
	return map[string]any{
		ActionIdentifierKey: "is.workflow.actions.downloadurl",
		ActionParametersKey: map[string]any{
			"WFURL":        "https://api.openai.com/v1/responses",
			"WFHTTPMethod": "POST",
			"WFHTTPHeaders": map[string]any{
				"Value": map[string]any{
					"WFDictionaryFieldValueItems": []any{
						map[string]any{
							"WFItemType": float64(0),
							"WFKey": map[string]any{
								"Value":               map[string]any{"string": headerName},
								"WFSerializationType": "WFTextTokenString",
							},
							"WFValue": map[string]any{
								"Value":               map[string]any{"string": headerValue},
								"WFSerializationType": "WFTextTokenString",
							},
						},
					},
				},
				"WFSerializationType": "WFDictionaryFieldValue",
			},
		},
	}
}

func TestScanPositives(t *testing.T) {
	tests := []struct {
		name     string
		action   map[string]any
		wantRule string
		secret   string
	}{
		{"openai key", textAction("My key is " + openAIKey), "openai-api-key", openAIKey},
		{"openai project key", textAction(openAIProjKey), "openai-api-key", openAIProjKey},
		{"anthropic key", textAction(anthropicKey), "anthropic-api-key", anthropicKey},
		{"github classic pat", textAction(githubPAT), "github-token", githubPAT},
		{"github oauth token", textAction(githubOAuth), "github-token", githubOAuth},
		{"github fine-grained pat", textAction(githubFinePAT), "github-token", githubFinePAT},
		{"aws access key id", textAction("aws_access_key_id=" + awsKeyID), "aws-access-key-id", awsKeyID},
		{"aws temporary key id", textAction(awsTempKeyID), "aws-access-key-id", awsTempKeyID},
		{"google api key", textAction("https://maps.googleapis.com/maps/api/geocode/json?key=" + googleKey), "google-api-key", googleKey},
		{"slack token", textAction(slackToken), "slack-token", slackToken},
		{"jwt", textAction(jwt), "jwt", jwt},
		{"pem private key", textAction(pemKey), "private-key-pem", "PRIVATE KEY"},
		{"bearer token", textAction("Authorization: Bearer " + bearerToken), "bearer-token", bearerToken},
		{"basic credentials", textAction("Authorization: Basic " + basicCreds), "basic-auth-credentials", basicCreds},
		{"generic api_key assignment", textAction(`{"api_key": "` + genericSecret + `"}`), "generic-secret-assignment", genericSecret},
		{"generic password assignment", textAction("password=" + genericSecret), "generic-secret-assignment", genericSecret},
		{"generic hyphenated api-key", textAction("api-key: " + genericSecret), "generic-secret-assignment", genericSecret},
		{"nested authorization header value", urlAction("Authorization", headerSecret), "authorization-value", headerSecret},
		{"nested x-api-key header value", urlAction("X-Api-Key", headerSecret), "authorization-value", headerSecret},
		{"nested openai key in header", urlAction("X-Custom", "Bearer "+openAIKey), "openai-api-key", openAIKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := Scan([]any{tt.action})
			if len(findings) == 0 {
				t.Fatalf("Scan() found nothing, want rule %q", tt.wantRule)
			}

			var matched *Finding
			for i := range findings {
				if findings[i].Rule == tt.wantRule {
					matched = &findings[i]
					break
				}
			}
			if matched == nil {
				t.Fatalf("Scan() = %v, want a %q finding", findings, tt.wantRule)
			}
			if matched.ActionIndex != 0 {
				t.Errorf("ActionIndex = %d, want 0", matched.ActionIndex)
			}
			if matched.Identifier != tt.action[ActionIdentifierKey] {
				t.Errorf("Identifier = %q, want %q", matched.Identifier, tt.action[ActionIdentifierKey])
			}
			if !strings.HasPrefix(matched.Path, ActionParametersKey+".") {
				t.Errorf("Path = %q, want it under %s", matched.Path, ActionParametersKey)
			}

			if strings.Contains(matched.Excerpt, tt.secret) {
				t.Errorf("Excerpt %q contains the secret", matched.Excerpt)
			}
			if report := Summary(findings); strings.Contains(report, tt.secret) {
				t.Errorf("Summary() leaked the secret:\n%s", report)
			}
		})
	}
}

func TestScanNegatives(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"plain prose", "Save the current shortcut to the backup repository and open the web review."},
		{"https url with query", "https://raw.githubusercontent.com/andrewmcodes/shortcut-source-saver/main/docs/index.html?cache=1"},
		{"api url without credentials", "https://api.github.com/repos/andrewmcodes/shortcut-source-saver/contents/docs"},
		{"hyphenated url path", "https://scikit-learn.org/stable/modules/generated/sklearn-preprocessing-normalize.html"},
		{"base64 image data", "data:image/png;base64," + base64Image},
		{"bare base64 blob", base64Image},
		{"uuid", "550E8400-E29B-41D4-A716-446655440000"},
		{"lowercase uuid list", "6c6e4c40-8a9f-4e67-9d9a-3e1d8c58f1a2, 9f2a1b0c-4d3e-4f50-8a6b-7c8d9e0f1a2b"},
		{"hex colours", "#1F2937 #F9FAFB #4F46E5"},
		{"file path", "/Users/andrew/Library/Shortcuts/Shortcuts.sqlite"},
		{"windows-ish path", "Documents/Shortcuts/Backups/2026-08-04/my-shortcut.shortcut"},
		{"sha1 digest", "9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c"},
		{"placeholder openai key", "sk-YOUR_OPENAI_API_KEY_GOES_HERE"},
		{"placeholder bearer", "Authorization: Bearer <your token here>"},
		{"placeholder angle brackets", "api_key = <INSERT_YOUR_KEY_HERE>"},
		{"placeholder changeme", `api_key: "changeme-before-first-run"`},
		{"placeholder xxx run", "token = xxxxxxxxxxxxxxxxxxxxxxxx"},
		{"placeholder example", `apikey = "example_key_for_the_docs_only"`},
		{"repeated character run", "password = ------------------------"},
		{"low entropy assignment", `token = "my_test_value_here"`},
		{"word-shaped assignment", "api_key = backup_repository_path"},
		{"path-shaped assignment", `secret: "docs/shortcuts/index/page"`},
		{"innocuous header-named value", "application/json; charset=utf-8"},
		{"prose about api keys", "Get your API key from the provider dashboard, then paste it into the prompt."},
		{"short token", "token = ab12cd34"},
		{"passphrase with spaces", "password = correct horse battery staple"},
		{"version numbers", "WFWorkflowClientVersion 2038.0.4.2, minimum 900"},
		{"emoji and text", "✅ Backed up 42 shortcuts to ~/git/my-shortcuts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if findings := Scan([]any{textAction(tt.value)}); len(findings) > 0 {
				t.Errorf("Scan(%q) = %v, want no findings", tt.value, findings)
			}

			if findings := Scan([]any{urlAction("Accept", tt.value)}); len(findings) > 0 {
				t.Errorf("Scan(header %q) = %v, want no findings", tt.value, findings)
			}
		})
	}
}

func TestScanIgnoresNonSecretActionShapes(t *testing.T) {
	actions := []any{
		"not a map",
		float64(42),
		nil,
		map[string]any{ActionIdentifierKey: "is.workflow.actions.comment"},
	}
	if findings := Scan(actions); len(findings) != 0 {
		t.Errorf("Scan() = %v, want no findings", findings)
	}
}

func TestScanRecursesAndRecordsIndexAndPath(t *testing.T) {
	actions := []any{
		textAction("Just a comment about the backup"),
		map[string]any{
			ActionIdentifierKey: "is.workflow.actions.dictionary",
			ActionParametersKey: map[string]any{
				"WFItems": map[string]any{
					"Value": map[string]any{
						"WFDictionaryFieldValueItems": []any{
							map[string]any{
								"WFKey":   map[string]any{"Value": map[string]any{"string": "credentials"}},
								"WFValue": map[string]any{"Value": map[string]any{"string": openAIKey}},
							},
						},
					},
				},
			},
		},
	}

	findings := Scan(actions)
	if len(findings) != 1 {
		t.Fatalf("Scan() = %v, want exactly one finding", findings)
	}
	got := findings[0]
	if got.ActionIndex != 1 {
		t.Errorf("ActionIndex = %d, want 1", got.ActionIndex)
	}
	if got.Identifier != "is.workflow.actions.dictionary" {
		t.Errorf("Identifier = %q", got.Identifier)
	}
	wantPath := ActionParametersKey + ".WFItems.Value.WFDictionaryFieldValueItems[0].WFValue.Value.string"
	if got.Path != wantPath {
		t.Errorf("Path = %q, want %q", got.Path, wantPath)
	}
}

func TestScanDeduplicatesRulesPerValue(t *testing.T) {

	findings := Scan([]any{textAction("Authorization: Bearer " + jwt)})
	if len(findings) != 1 {
		t.Fatalf("Scan() = %v, want exactly one finding", findings)
	}
	if findings[0].Rule != "jwt" {
		t.Errorf("Rule = %q, want jwt", findings[0].Rule)
	}
}

func TestScanAnthropicKeyIsNotReportedTwice(t *testing.T) {
	findings := Scan([]any{textAction(anthropicKey)})
	if len(findings) != 1 {
		t.Fatalf("Scan() = %v, want exactly one finding", findings)
	}
	if findings[0].Rule != "anthropic-api-key" {
		t.Errorf("Rule = %q, want anthropic-api-key", findings[0].Rule)
	}
}

func TestRedact(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{"long key", openAIKey},
		{"short value", "abcd"},
		{"exactly the prefix", "abcdef"},
		{"multiline pem", pemKey},
		{"multibyte", "ünïcödë-sëcrët-vàlüe-1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.secret)
			if strings.Contains(got, tt.secret) && len([]rune(tt.secret)) > redactPrefix {
				t.Errorf("Redact(%q) = %q, want the tail masked", tt.secret, got)
			}
			if strings.Contains(got, "\n") {
				t.Errorf("Redact() = %q, want a single line", got)
			}
			if len([]rune(tt.secret)) > redactPrefix {
				if prefix := string([]rune(tt.secret)[:redactPrefix]); !strings.HasPrefix(got, prefix) {
					t.Errorf("Redact(%q) = %q, want prefix %q", tt.secret, got, prefix)
				}
			}
		})
	}
}

func TestEntropy(t *testing.T) {
	tests := []struct {
		name  string
		value string
		below bool
	}{
		{"random key", genericSecret, false},
		{"base64 blob", base64Image, false},
		{"repeated word", "password_password_password", true},
		{"repeated character", strings.Repeat("x", 32), true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Entropy(tt.value)
			if below := got < minGenericEntropy; below != tt.below {
				t.Errorf("Entropy(%q) = %.2f, want below=%v against threshold %.1f", tt.value, got, tt.below, minGenericEntropy)
			}
		})
	}
}

func TestLooksLikeWords(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"my_test_value_here", true},
		{"backup_repository_path", true},
		{"shortcut-source-saver", true},
		{"docs/index/page", true},
		{genericSecret, false},
		{base64Image, false},
		{"550E8400-E29B-41D4-A716-446655440000", false},
		{"singleword", false},
		{"a_b_c", false},
	}
	for _, tt := range tests {
		t.Run(tt.value[:min(len(tt.value), 24)], func(t *testing.T) {
			if got := looksLikeWords(tt.value); got != tt.want {
				t.Errorf("looksLikeWords(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	if got := Summary(nil); got != "" {
		t.Errorf("Summary(nil) = %q, want empty", got)
	}

	findings := Scan([]any{
		textAction("Just prose"),
		textAction("key: " + openAIKey),
		urlAction("Authorization", "Bearer "+bearerToken),
	})
	report := Summary(findings)

	for _, want := range []string{"action 1", "action 2", "openai-api-key", "bearer-token", "is.workflow.actions.gettext", "is.workflow.actions.downloadurl"} {
		if !strings.Contains(report, want) {
			t.Errorf("Summary() missing %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "action 0") {
		t.Errorf("Summary() reported a clean action:\n%s", report)
	}
	if strings.Contains(report, openAIKey) || strings.Contains(report, bearerToken) {
		t.Errorf("Summary() leaked a secret:\n%s", report)
	}
	if !strings.HasPrefix(report, "2 possible credentials in 2 actions") {
		t.Errorf("Summary() header = %q", strings.SplitN(report, "\n", 2)[0])
	}
}

func TestRulesAreNamedAndDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, rule := range Rules() {
		if rule.Name == "" || rule.Description == "" {
			t.Errorf("rule %+v is missing a name or description", rule)
		}
		if seen[rule.Name] {
			t.Errorf("duplicate rule name %q", rule.Name)
		}
		seen[rule.Name] = true
	}
	for _, want := range []string{"openai-api-key", "anthropic-api-key", "github-token", "aws-access-key-id", "google-api-key", "slack-token", "jwt", "private-key-pem", "bearer-token", "generic-secret-assignment"} {
		if !seen[want] {
			t.Errorf("missing rule %q", want)
		}
	}
}

func TestScanValue(t *testing.T) {
	findings := ScanValue(7, "import-questions", []any{
		map[string]any{"Text": "Paste your key", "DefaultValue": openAIKey},
	})
	if len(findings) != 1 {
		t.Fatalf("ScanValue() = %v, want one finding", findings)
	}
	if findings[0].ActionIndex != 7 || findings[0].Identifier != "import-questions" {
		t.Errorf("ScanValue() = %+v, want index 7 and the given identifier", findings[0])
	}
	if findings[0].Path != "[0].DefaultValue" {
		t.Errorf("Path = %q, want [0].DefaultValue", findings[0].Path)
	}
}
