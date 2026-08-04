package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func isolate(t *testing.T) (home, xdg string) {
	t.Helper()
	home = t.TempDir()
	xdg = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	return home, xdg
}

func TestSearchPaths(t *testing.T) {
	t.Run("uses XDG_CONFIG_HOME when set", func(t *testing.T) {
		_, xdg := isolate(t)
		paths := SearchPaths()
		want := []string{
			filepath.Join(xdg, AppName, FileName),
			filepath.Join("/etc", AppName, FileName),
		}
		if len(paths) != len(want) {
			t.Fatalf("SearchPaths() = %v, want %v", paths, want)
		}
		for i := range want {
			if paths[i] != want[i] {
				t.Errorf("SearchPaths()[%d] = %q, want %q", i, paths[i], want[i])
			}
		}
	})

	t.Run("falls back to ~/.config", func(t *testing.T) {
		home, _ := isolate(t)
		t.Setenv("XDG_CONFIG_HOME", "")
		paths := SearchPaths()
		if want := filepath.Join(home, ".config", AppName, FileName); paths[0] != want {
			t.Errorf("SearchPaths()[0] = %q, want %q", paths[0], want)
		}
		if want := filepath.Join("/etc", AppName, FileName); paths[len(paths)-1] != want {
			t.Errorf("last search path = %q, want %q", paths[len(paths)-1], want)
		}
	})
}

func TestLoadDefaults(t *testing.T) {
	isolate(t)
	if _, err := os.Stat(filepath.Join("/etc", AppName, FileName)); err == nil {
		t.Skip("machine-wide config installed; cannot test the no-file case")
	}

	_, err := Load("")
	if !errors.Is(err, ErrMissingRepoPath) {
		t.Fatalf("Load() error = %v, want ErrMissingRepoPath", err)
	}

	_, xdg := isolate(t)
	writeConfig(t, filepath.Join(xdg, AppName, FileName), "repo_path = \"/tmp/repo\"\n")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Default()
	want.RepoPath = "/tmp/repo"
	want.Path = filepath.Join(xdg, AppName, FileName)
	if *cfg != *want {
		t.Errorf("Load() = %+v, want %+v", *cfg, *want)
	}
}

func TestLoadPrecedence(t *testing.T) {
	home, xdg := isolate(t)

	xdgPath := writeConfig(t, filepath.Join(xdg, AppName, FileName), `
repo_path  = "/tmp/from-xdg"
site_title = "From XDG"
`)

	writeConfig(t, filepath.Join(home, ".config", AppName, FileName), `repo_path = "/tmp/from-home"`)
	explicitPath := writeConfig(t, filepath.Join(t.TempDir(), "explicit.toml"), `
repo_path  = "/tmp/from-explicit"
site_title = "From Explicit"
auto_push  = true
`)

	tests := []struct {
		name      string
		explicit  string
		wantRepo  string
		wantTitle string
		wantPath  string
		wantPush  bool
	}{
		{"xdg wins over home fallback", "", "/tmp/from-xdg", "From XDG", xdgPath, false},
		{"explicit wins over everything", explicitPath, "/tmp/from-explicit", "From Explicit", explicitPath, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(tt.explicit)
			if err != nil {
				t.Fatalf("Load(%q) error = %v", tt.explicit, err)
			}
			if cfg.RepoPath != tt.wantRepo {
				t.Errorf("RepoPath = %q, want %q", cfg.RepoPath, tt.wantRepo)
			}
			if cfg.SiteTitle != tt.wantTitle {
				t.Errorf("SiteTitle = %q, want %q", cfg.SiteTitle, tt.wantTitle)
			}
			if cfg.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", cfg.Path, tt.wantPath)
			}
			if cfg.AutoPush != tt.wantPush {
				t.Errorf("AutoPush = %v, want %v", cfg.AutoPush, tt.wantPush)
			}
		})
	}

	t.Run("home fallback used when XDG unset", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.RepoPath != "/tmp/from-home" {
			t.Errorf("RepoPath = %q, want /tmp/from-home", cfg.RepoPath)
		}
	})
}

func TestLoadExplicitMissingIsError(t *testing.T) {
	isolate(t)
	missing := filepath.Join(t.TempDir(), "nope.toml")
	if _, err := Load(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load(%q) error = %v, want os.ErrNotExist", missing, err)
	}
}

func TestLoadFile(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
		check   func(t *testing.T, cfg *Config)
	}{
		{
			name: "tilde expands in repo_path",
			body: `repo_path = "~/git/my-shortcuts"`,
			check: func(t *testing.T, cfg *Config) {
				want := filepath.Join(os.Getenv("HOME"), "git", "my-shortcuts")
				if cfg.RepoPath != want {
					t.Errorf("RepoPath = %q, want %q", cfg.RepoPath, want)
				}
				if strings.Contains(cfg.RepoPath, "~") {
					t.Errorf("RepoPath %q still contains a tilde", cfg.RepoPath)
				}
			},
		},
		{
			name: "bare tilde expands to home",
			body: `repo_path = "~"`,
			check: func(t *testing.T, cfg *Config) {
				if cfg.RepoPath != os.Getenv("HOME") {
					t.Errorf("RepoPath = %q, want %q", cfg.RepoPath, os.Getenv("HOME"))
				}
			},
		},
		{
			name:    "relative repo_path rejected",
			body:    `repo_path = "my-shortcuts"`,
			wantErr: "must be an absolute path",
		},
		{
			name:    "dot-relative repo_path rejected",
			body:    `repo_path = "./backups"`,
			wantErr: "must be an absolute path",
		},
		{
			name:    "empty repo_path rejected",
			body:    `site_title = "No repo"`,
			wantErr: "repo_path is required",
		},
		{
			name:    "unknown key rejected",
			body:    "repo_path = \"/tmp/repo\"\nautopush = true\nsitedir = \"web\"\n",
			wantErr: "unknown key(s): autopush, sitedir",
		},
		{
			name:    "invalid sign_mode rejected",
			body:    "repo_path = \"/tmp/repo\"\nsign_mode = \"friends\"\n",
			wantErr: `sign_mode "friends" is invalid`,
		},
		{
			name: "sign_mode people-who-know-me accepted",
			body: "repo_path = \"/tmp/repo\"\nsign_mode = \"people-who-know-me\"\n",
			check: func(t *testing.T, cfg *Config) {
				if cfg.SignMode != SignModePeopleWhoKnowMe {
					t.Errorf("SignMode = %q", cfg.SignMode)
				}
			},
		},
		{
			name: "base_url gets both slashes",
			body: "repo_path = \"/tmp/repo\"\nbase_url = \"my-shortcuts\"\n",
			check: func(t *testing.T, cfg *Config) {
				if cfg.BaseURL != "/my-shortcuts/" {
					t.Errorf("BaseURL = %q, want /my-shortcuts/", cfg.BaseURL)
				}
			},
		},
		{
			name: "empty base_url becomes root",
			body: "repo_path = \"/tmp/repo\"\nbase_url = \"\"\n",
			check: func(t *testing.T, cfg *Config) {
				if cfg.BaseURL != "/" {
					t.Errorf("BaseURL = %q, want /", cfg.BaseURL)
				}
			},
		},
		{
			name: "base_url already normalised is untouched",
			body: "repo_path = \"/tmp/repo\"\nbase_url = \"/nested/path/\"\n",
			check: func(t *testing.T, cfg *Config) {
				if cfg.BaseURL != "/nested/path/" {
					t.Errorf("BaseURL = %q, want /nested/path/", cfg.BaseURL)
				}
			},
		},
		{
			name:    "empty site_dir rejected",
			body:    "repo_path = \"/tmp/repo\"\nsite_dir = \"\"\n",
			wantErr: "site_dir must not be empty",
		},
		{
			name: "booleans can be turned off",
			body: "repo_path = \"/tmp/repo\"\nauto_commit = false\ninclude_signed = false\ninclude_hidden = true\n",
			check: func(t *testing.T, cfg *Config) {
				if cfg.AutoCommit || cfg.IncludeSigned || !cfg.IncludeHidden {
					t.Errorf("booleans = %+v", cfg)
				}
			},
		},
		{
			name:    "malformed TOML reported with path",
			body:    "repo_path = ",
			wantErr: "config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			path := writeConfig(t, filepath.Join(t.TempDir(), FileName), tt.body)

			cfg, err := Load(path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() = %+v, want error containing %q", cfg, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			tt.check(t, cfg)
		})
	}
}

func TestSitePath(t *testing.T) {
	home, _ := isolate(t)

	tests := []struct {
		name    string
		siteDir string
		want    string
	}{
		{"relative resolves against repo_path", "docs", filepath.Join(home, "repo", "docs")},
		{"nested relative", "web/site", filepath.Join(home, "repo", "web", "site")},
		{"absolute used as-is", "/srv/www/", "/srv/www"},
		{"tilde expanded", "~/public", filepath.Join(home, "public")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, filepath.Join(t.TempDir(), FileName),
				"repo_path = \"~/repo\"\nsite_dir = \""+tt.siteDir+"\"\n")
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got := cfg.SitePath(); got != tt.want {
				t.Errorf("SitePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExampleIsValid(t *testing.T) {
	isolate(t)
	path := writeConfig(t, filepath.Join(t.TempDir(), FileName), Example())

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(Example()) error = %v", err)
	}
	if want := filepath.Join(os.Getenv("HOME"), "git", "my-shortcuts"); cfg.RepoPath != want {
		t.Errorf("RepoPath = %q, want %q", cfg.RepoPath, want)
	}
	if cfg.AutoPush {
		t.Error("example config enables auto_push; it should default off")
	}
	for _, key := range []string{"repo_path", "sign_mode", "site_dir", "site_title", "base_url", "auto_commit", "auto_push", "include_signed", "include_hidden"} {
		if !strings.Contains(Example(), key) {
			t.Errorf("Example() is missing %q", key)
		}
	}
}

func TestValidateNormalisesInPlace(t *testing.T) {
	cfg := Default()
	cfg.RepoPath = "/tmp/repo"
	cfg.BaseURL = "//sss//"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.BaseURL != "/sss/" {
		t.Errorf("BaseURL = %q, want /sss/", cfg.BaseURL)
	}
}
