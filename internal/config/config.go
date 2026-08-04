package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	AppName  = "shortcut-source-saver"
	FileName = "config.toml"

	SignModeAnyone          = "anyone"
	SignModePeopleWhoKnowMe = "people-who-know-me"
)

type Config struct {
	RepoPath      string `toml:"repo_path"`
	SignMode      string `toml:"sign_mode"`
	SiteDir       string `toml:"site_dir"`
	SiteTitle     string `toml:"site_title"`
	BaseURL       string `toml:"base_url"`
	AutoCommit    bool   `toml:"auto_commit"`
	AutoPush      bool   `toml:"auto_push"`
	IncludeSigned bool   `toml:"include_signed"`
	IncludeHidden bool   `toml:"include_hidden"`

	Path string `toml:"-"`
}

var ErrMissingRepoPath = errors.New("repo_path is required")

func Default() *Config {
	return &Config{
		RepoPath:      "",
		SignMode:      SignModeAnyone,
		SiteDir:       "docs",
		SiteTitle:     "My Shortcuts",
		BaseURL:       "/",
		AutoCommit:    true,
		AutoPush:      false,
		IncludeSigned: true,
		IncludeHidden: false,
	}
}

func SearchPaths() []string {
	var paths []string
	if dir := userConfigDir(); dir != "" {
		paths = append(paths, filepath.Join(dir, AppName, FileName))
	}
	return append(paths, filepath.Join("/etc", AppName, FileName))
}

func userConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

func Load(explicitPath string) (*Config, error) {
	cfg := Default()

	if explicitPath != "" {
		path, err := ExpandTilde(explicitPath)
		if err != nil {
			return nil, err
		}
		if err := cfg.decodeFile(path); err != nil {
			return nil, err
		}
		return cfg, cfg.resolve()
	}

	for _, path := range SearchPaths() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := cfg.decodeFile(path); err != nil {
			return nil, err
		}
		break
	}
	return cfg, cfg.resolve()
}

func (c *Config) decodeFile(path string) error {
	md, err := toml.DecodeFile(path, c)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("config file %s: %w", path, os.ErrNotExist)
		}
		return fmt.Errorf("config file %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}
		sort.Strings(keys)
		return fmt.Errorf("config file %s: unknown key(s): %s (check for typos; run `sss init` to see every supported key)",
			path, strings.Join(keys, ", "))
	}
	c.Path = path
	return nil
}

func (c *Config) resolve() error {
	repo, err := ExpandTilde(c.RepoPath)
	if err != nil {
		return err
	}
	c.RepoPath = repo

	if strings.HasPrefix(c.SiteDir, "~") {
		site, err := ExpandTilde(c.SiteDir)
		if err != nil {
			return err
		}
		c.SiteDir = site
	}

	return c.Validate()
}

func ExpandTilde(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot expand %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.RepoPath) == "" {
		return ErrMissingRepoPath
	}
	if !filepath.IsAbs(c.RepoPath) {
		return fmt.Errorf("repo_path %q must be an absolute path (use a leading ~/ or a full path such as /Users/you/git/shortcuts)", c.RepoPath)
	}
	switch c.SignMode {
	case SignModeAnyone, SignModePeopleWhoKnowMe:
	default:
		return fmt.Errorf("sign_mode %q is invalid: want %q or %q", c.SignMode, SignModeAnyone, SignModePeopleWhoKnowMe)
	}
	if strings.TrimSpace(c.SiteDir) == "" {
		return errors.New("site_dir must not be empty")
	}
	c.BaseURL = normalizeBaseURL(c.BaseURL)
	return nil
}

func normalizeBaseURL(raw string) string {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "/"
	}
	return "/" + trimmed + "/"
}

func (c *Config) SitePath() string {
	if filepath.IsAbs(c.SiteDir) {
		return filepath.Clean(c.SiteDir)
	}
	return filepath.Join(c.RepoPath, c.SiteDir)
}

func Example() string {
	return `# shortcut-source-saver configuration
#
# Searched in order (first match wins), unless --config is given:
#   $XDG_CONFIG_HOME/` + AppName + `/` + FileName + `  (default: ~/.config/` + AppName + `/` + FileName + `)
#   /etc/` + AppName + `/` + FileName + `

# Where the backup git repository lives. Required, and must be absolute;
# a leading ~/ is expanded to your home directory.
repo_path = "~/git/my-shortcuts"

# Audience for the generated .shortcut files: "anyone" or "people-who-know-me".
sign_mode = "` + SignModeAnyone + `"

# Generated GitHub Pages site, relative to repo_path (an absolute path is used as-is).
site_dir = "docs"

# Title shown on the generated site.
site_title = "My Shortcuts"

# URL prefix the site is served under. Use "/<repo-name>/" for a GitHub Pages
# project site, "/" for a user or organisation site.
base_url = "/"

# Commit generated files after a successful backup.
auto_commit = true

# Push after committing. Off by default: a backup repository is often public and
# a published API key cannot be unpublished.
auto_push = false

# Write the importable .shortcut file alongside the JSON.
include_signed = true

# Include shortcuts hidden from the Shortcuts library.
include_hidden = false
`
}
