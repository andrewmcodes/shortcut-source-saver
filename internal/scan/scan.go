package scan

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	ActionIdentifierKey = "WFWorkflowActionIdentifier"
	ActionParametersKey = "WFWorkflowActionParameters"
)

const (
	minGenericEntropy = 3.2
	minGenericLength  = 16
	redactPrefix      = 6
	wordSeparators    = "_-./ "
)

var placeholderMarkers = []string{
	"your_", "your-", "yourapi", "yourtoken", "youraccess", "your api", "your token",
	"example", "changeme", "change_me", "placeholder", "dummy", "sample", "insert",
	"redacted", "removed", "todo", "fixme", "fake", "notreal", "abc123", "foobar",
	"<", ">", "…", "***", "api_key_here", "key_here", "token_here", "secret_here",
	"xxxx", "0000", "1234567890", "test_test",
}

type Finding struct {
	ActionIndex int
	Identifier  string
	Path        string
	Rule        string
	Excerpt     string
}

func (f Finding) String() string {
	return fmt.Sprintf("action %d (%s): %s matched %s at %s", f.ActionIndex, f.Identifier, f.Rule, f.Excerpt, f.Path)
}

type Rule struct {
	Name        string
	Description string

	pattern    *regexp.Regexp
	group      int
	exclude    *regexp.Regexp
	minEntropy float64
}

var rules = []Rule{
	{
		Name:        "anthropic-api-key",
		Description: "Anthropic API key (sk-ant-…)",
		pattern:     regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{16,}`),
	},
	{
		Name:        "openai-api-key",
		Description: "OpenAI API key (sk-… or sk-proj-…)",
		pattern:     regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}`),
		exclude:     regexp.MustCompile(`^sk-ant-`),
	},
	{
		Name:        "github-token",
		Description: "GitHub personal access, OAuth, user, server or refresh token",
		pattern:     regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{22,})\b`),
	},
	{
		Name:        "aws-access-key-id",
		Description: "AWS access key id (AKIA/ASIA…)",
		pattern:     regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	},
	{
		Name:        "google-api-key",
		Description: "Google API key (AIza…)",
		pattern:     regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	},
	{
		Name:        "slack-token",
		Description: "Slack token (xoxb-, xoxa-, xoxp-, xoxr-, xoxs-)",
		pattern:     regexp.MustCompile(`\bxox[abprs]-[0-9A-Za-z-]{10,}`),
	},
	{
		Name:        "private-key-pem",
		Description: "PEM private key block",
		pattern:     regexp.MustCompile(`-----BEGIN (?:RSA |DSA |EC |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----`),
	},
	{
		Name:        "jwt",
		Description: "JSON Web Token (three base64url segments after an eyJ header)",
		pattern:     regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	},
	{
		Name:        "bearer-token",
		Description: "Bearer token in an Authorization-like string",
		pattern:     regexp.MustCompile(`(?i)\b(?:bearer|token)\s+([A-Za-z0-9_.\-+/]{20,}={0,2})`),
		group:       1,
		minEntropy:  minGenericEntropy,
	},
	{
		Name:        "basic-auth-credentials",
		Description: "HTTP Basic credentials (Basic <base64>)",
		pattern:     regexp.MustCompile(`(?i)\bbasic\s+([A-Za-z0-9+/]{16,}={0,2})`),
		group:       1,
		minEntropy:  minGenericEntropy,
	},
	{
		Name:        "generic-secret-assignment",
		Description: "api_key/apikey/token/secret/password assigned a high-entropy value",
		pattern:     genericSecretPattern,
		group:       1,
		minEntropy:  minGenericEntropy,
	},
}

var genericSecretPattern = regexp.MustCompile(fmt.Sprintf(
	`(?i)\b(?:api[_-]?key|apikey|access[_-]?token|auth[_-]?token|secret[_-]?key|client[_-]?secret|secret|token|password|passwd|pwd)\b["']?\s*[:=]\s*["']?([A-Za-z0-9_.\-+/~]{%d,}={0,2})`,
	minGenericLength))

var authorizationKeyPattern = regexp.MustCompile(`(?i)(authorization|api[_-]?key|apikey|access[_-]?token|auth[_-]?token|x-auth|secret|password|passwd|token)$`)

func Rules() []Rule {
	out := make([]Rule, len(rules))
	copy(out, rules)
	return out
}

func Scan(actions []any) []Finding {
	var findings []Finding
	for i, action := range actions {
		m, ok := action.(map[string]any)
		if !ok {
			continue
		}
		identifier, _ := m[ActionIdentifierKey].(string)
		findings = append(findings, scanAction(i, identifier, m)...)
	}
	return findings
}

func ScanValue(index int, identifier string, value any) []Finding {
	return scanAction(index, identifier, value)
}

func scanAction(index int, identifier string, action any) []Finding {
	var findings []Finding
	seen := make(map[string]bool)

	walk(action, "", "", func(path, context, value string) {
		for _, f := range match(path, context, value) {
			f.ActionIndex = index
			f.Identifier = identifier
			key := f.Path + "\x00" + f.Excerpt
			if seen[key] {
				continue
			}
			seen[key] = true
			findings = append(findings, f)
		}
	})
	return findings
}

func match(path, context, value string) []Finding {
	var findings []Finding
	reported := make(map[string]bool)

	for _, rule := range rules {
		for _, groups := range rule.pattern.FindAllStringSubmatch(value, -1) {
			secret := groups[0]
			if rule.group > 0 && rule.group < len(groups) {
				secret = groups[rule.group]
			}
			if !plausibleSecret(secret, rule) {
				continue
			}
			if reported[secret] {
				continue
			}
			reported[secret] = true
			findings = append(findings, Finding{Path: path, Rule: rule.Name, Excerpt: Redact(secret)})
		}
	}

	if len(findings) == 0 && authorizationKeyPattern.MatchString(context) {
		secret := strings.TrimSpace(value)
		if plausibleSecret(secret, Rule{minEntropy: minGenericEntropy}) {
			findings = append(findings, Finding{Path: path, Rule: "authorization-value", Excerpt: Redact(secret)})
		}
	}
	return findings
}

func plausibleSecret(secret string, rule Rule) bool {
	if rule.exclude != nil && rule.exclude.MatchString(secret) {
		return false
	}
	if isPlaceholder(secret) {
		return false
	}
	if rule.minEntropy > 0 {
		if len(secret) < minGenericLength {
			return false
		}
		if looksLikeWords(secret) {
			return false
		}
		if Entropy(secret) < rule.minEntropy {
			return false
		}
	}
	return true
}

func isPlaceholder(secret string) bool {
	lower := strings.ToLower(secret)
	for _, marker := range placeholderMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	distinct := make(map[rune]struct{}, len(secret))
	for _, r := range secret {
		distinct[r] = struct{}{}
	}
	return len(distinct) <= 2
}

func looksLikeWords(secret string) bool {
	segments := strings.FieldsFunc(secret, func(r rune) bool {
		return strings.ContainsRune(wordSeparators, r)
	})
	if len(segments) < 2 {
		return false
	}
	for _, segment := range segments {
		if len(segment) < 2 {
			return false
		}
		for _, r := range segment {
			if !unicode.IsLetter(r) {
				return false
			}
		}
	}
	return true
}

func Entropy(s string) float64 {
	if s == "" {
		return 0
	}
	counts := make(map[rune]int, len(s))
	total := 0
	for _, r := range s {
		counts[r]++
		total++
	}
	var bits float64
	for _, n := range counts {
		p := float64(n) / float64(total)
		bits -= p * math.Log2(p)
	}
	return bits
}

func Redact(secret string) string {
	flat := strings.Join(strings.Fields(secret), " ")
	runes := []rune(flat)
	if len(runes) <= redactPrefix {
		return strings.Repeat("*", len(runes)) + fmt.Sprintf(" (%d chars)", len(runes))
	}
	return string(runes[:redactPrefix]) + "******" + fmt.Sprintf(" (%d chars)", len(runes))
}

var wrapperKeys = map[string]bool{
	"Value":                       true,
	"WFValue":                     true,
	"string":                      true,
	"WFItems":                     true,
	"WFDictionaryFieldValueItems": true,
	"attachmentsByRange":          true,
}

func walk(value any, path, context string, visit func(path, context, value string)) {
	switch v := value.(type) {
	case string:
		if v != "" {
			visit(path, context, v)
		}
	case map[string]any:
		walkMap(v, path, context, visit)
	case map[any]any:
		converted := make(map[string]any, len(v))
		for k, val := range v {
			converted[fmt.Sprint(k)] = val
		}
		walkMap(converted, path, context, visit)
	case []any:
		for i, item := range v {
			walk(item, fmt.Sprintf("%s[%d]", path, i), context, visit)
		}
	case []string:
		for i, item := range v {
			walk(item, fmt.Sprintf("%s[%d]", path, i), context, visit)
		}
	}
}

func walkMap(m map[string]any, path, context string, visit func(path, context, value string)) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	itemKey := ""
	if _, ok := m["WFValue"]; ok {
		itemKey = firstString(m["WFKey"])
	}

	for _, k := range keys {
		childContext := k
		switch {
		case k == "WFValue" && itemKey != "":
			childContext = itemKey
		case wrapperKeys[k] && context != "":
			childContext = context
		}
		walk(m[k], joinPath(path, k), childContext, visit)
	}
}

func firstString(value any) string {
	found := ""
	walk(value, "", "", func(_, _, s string) {
		if found == "" {
			found = s
		}
	})
	return found
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func Summary(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}

	byAction := make(map[int][]Finding)
	var order []int
	for _, f := range findings {
		if _, ok := byAction[f.ActionIndex]; !ok {
			order = append(order, f.ActionIndex)
		}
		byAction[f.ActionIndex] = append(byAction[f.ActionIndex], f)
	}
	sort.Ints(order)

	var b strings.Builder
	fmt.Fprintf(&b, "%s in %s\n", plural(len(findings), "possible credential", "possible credentials"),
		plural(len(order), "action", "actions"))
	for _, index := range order {
		group := byAction[index]
		identifier := group[0].Identifier
		if identifier == "" {
			identifier = "unknown action"
		}
		fmt.Fprintf(&b, "\naction %d (%s)\n", index, identifier)
		for _, f := range group {
			fmt.Fprintf(&b, "  %-26s %s\n      at %s\n", f.Rule, f.Excerpt, f.Path)
		}
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
