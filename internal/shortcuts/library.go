// Package shortcuts reads the local Apple Shortcuts library.
//
// Shortcuts has no export command (`shortcuts` only offers run/list/view/sign), so the
// only way to get a shortcut's contents is to read the Core Data store at
// ~/Library/Shortcuts/Shortcuts.sqlite. Everything that knows about that private schema
// lives in this package.
package shortcuts

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// coreDataEpoch is the reference date Core Data stores timestamps against.
var coreDataEpoch = time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)

// ErrNoAccess reports that the Shortcuts database exists but could not be read, which on
// macOS almost always means the calling terminal lacks Full Disk Access.
var ErrNoAccess = errors.New("cannot read the Shortcuts database")

// Shortcut is one entry in the library, without its actions.
type Shortcut struct {
	Name        string
	WorkflowID  string
	ActionCount int
	Created     time.Time
	Modified    time.Time
	Icon        Icon
	Hidden      bool

	pk int64
}

// Icon is a shortcut's glyph and background colour.
type Icon struct {
	Glyph int64
	// Color is the raw ZBACKGROUNDCOLORVALUE masked to 32 bits. The column stores the
	// same colour as both a positive and a negative integer depending on when it was
	// written, so it must be masked rather than interpreted as signed or unsigned.
	Color uint32
}

// Workflow is a shortcut plus everything needed to rebuild an importable .shortcut file.
type Workflow struct {
	Shortcut

	Actions                  []any
	InputContentItemClasses  []any
	OutputContentItemClasses []any
	ImportQuestions          []any

	ClientVersion        string
	MinimumClientVersion string

	HasOutputFallback         bool
	HasShortcutInputVariables bool
	ShowInSearch              bool
	HiddenFromWidget          bool
}

// Library is a read-only snapshot of the Shortcuts database. Close removes the snapshot.
type Library struct {
	db      *sql.DB
	tempDir string
}

// DefaultPath returns the location of the live Shortcuts database.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Shortcuts", "Shortcuts.sqlite"), nil
}

// Open snapshots the Shortcuts database and opens the copy read-only.
//
// The snapshot is not an optimisation. Shortcuts keeps the database in WAL mode and
// leaves recent edits sitting in Shortcuts.sqlite-wal, so opening the live file with
// SQLite's immutable=1 flag silently returns stale data: a freshly added shortcut is
// simply absent from the results. Copying the database together with its -wal and -shm
// sidecars and then opening the copy normally lets SQLite replay the WAL, which is the
// only way to see the library as the Shortcuts app sees it. We copy rather than read in
// place so that a concurrently running Shortcuts app cannot checkpoint the WAL out from
// under us mid-read.
func Open(path string) (*Library, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return nil, err
		}
	}

	tempDir, err := os.MkdirTemp("", "sss-snapshot-")
	if err != nil {
		return nil, err
	}

	snapshot := filepath.Join(tempDir, "Shortcuts.sqlite")
	if err := copyFile(path, snapshot); err != nil {
		os.RemoveAll(tempDir)
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("%w at %s: grant your terminal Full Disk Access in "+
				"System Settings > Privacy & Security > Full Disk Access, then try again", ErrNoAccess, path)
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no Shortcuts database at %s: open the Shortcuts app once to create it", path)
		}
		return nil, err
	}
	// The sidecars are optional: they are absent when the database was checkpointed
	// cleanly, and their absence is not an error.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyFile(path+suffix, snapshot+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			os.RemoveAll(tempDir)
			return nil, fmt.Errorf("copying %s%s: %w", filepath.Base(path), suffix, err)
		}
	}

	db, err := sql.Open("sqlite", "file:"+snapshot+"?mode=ro")
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, err
	}
	lib := &Library{db: db, tempDir: tempDir}
	if err := lib.verifySchema(); err != nil {
		lib.Close()
		return nil, err
	}
	return lib, nil
}

// Close closes the database and deletes the snapshot.
func (l *Library) Close() error {
	err := l.db.Close()
	if rmErr := os.RemoveAll(l.tempDir); err == nil {
		err = rmErr
	}
	return err
}

// verifySchema fails early with an actionable message if macOS has changed the private
// Core Data schema out from under us, rather than surfacing a bare SQLite error later.
func (l *Library) verifySchema() error {
	required := map[string][]string{
		"ZSHORTCUT":        {"ZNAME", "ZWORKFLOWID", "ZACTIONS", "ZICON", "ZTOMBSTONED"},
		"ZSHORTCUTACTIONS": {"ZDATA"},
		"ZSHORTCUTICON":    {"ZGLYPHNUMBER", "ZBACKGROUNDCOLORVALUE"},
	}
	for table, columns := range required {
		rows, err := l.db.Query("SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			have[name] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(have) == 0 {
			return fmt.Errorf("table %s is missing: this macOS version stores Shortcuts differently "+
				"than this tool understands", table)
		}
		for _, c := range columns {
			if !have[c] {
				return fmt.Errorf("column %s.%s is missing: this macOS version stores Shortcuts "+
					"differently than this tool understands", table, c)
			}
		}
	}
	return nil
}

const listQuery = `
SELECT s.Z_PK, s.ZNAME, s.ZWORKFLOWID, s.ZACTIONCOUNT,
       s.ZCREATIONDATE, s.ZMODIFICATIONDATE,
       COALESCE(i.ZGLYPHNUMBER, 0), COALESCE(i.ZBACKGROUNDCOLORVALUE, 0),
       COALESCE(s.ZHIDDENFROMLIBRARYANDSYNC, 0)
FROM ZSHORTCUT s
LEFT JOIN ZSHORTCUTICON i ON i.Z_PK = s.ZICON
WHERE COALESCE(s.ZTOMBSTONED, 0) = 0 AND s.ZNAME IS NOT NULL AND s.ZACTIONS IS NOT NULL
ORDER BY s.ZNAME COLLATE NOCASE`

// List returns every shortcut in the library. Shortcuts hidden from the library and from
// iCloud sync are included but flagged; callers decide whether to skip them.
func (l *Library) List() ([]Shortcut, error) {
	rows, err := l.db.Query(listQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Shortcut
	for rows.Next() {
		var (
			s        Shortcut
			created  sql.NullFloat64
			modified sql.NullFloat64
			count    sql.NullInt64
			color    int64
			hidden   int64
		)
		if err := rows.Scan(&s.pk, &s.Name, &s.WorkflowID, &count, &created, &modified,
			&s.Icon.Glyph, &color, &hidden); err != nil {
			return nil, err
		}
		s.ActionCount = int(count.Int64)
		s.Created = coreDataTime(created)
		s.Modified = coreDataTime(modified)
		s.Icon.Color = uint32(color)
		s.Hidden = hidden == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

const loadQuery = `
SELECT a.ZDATA,
       s.ZINPUTCLASSESDATA, s.ZOUTPUTCLASSESDATA, s.ZIMPORTQUESTIONSDATA,
       COALESCE(s.ZLASTMIGRATEDCLIENTVERSION, ''), COALESCE(s.ZMINIMUMCLIENTVERSION, ''),
       COALESCE(s.ZHASOUTPUTFALLBACK, 0), COALESCE(s.ZHASSHORTCUTINPUTVARIABLES, 0),
       COALESCE(s.ZSHOWINSEARCH, 0), COALESCE(s.ZHIDDENFROMWIDGET, 0)
FROM ZSHORTCUT s
JOIN ZSHORTCUTACTIONS a ON a.Z_PK = s.ZACTIONS
WHERE s.Z_PK = ?`

// Load reads a shortcut's actions and the metadata needed to rebuild a .shortcut file.
func (l *Library) Load(s Shortcut) (*Workflow, error) {
	var (
		actions                    []byte
		inputs, outputs, questions []byte
		fallback, inputVars        int64
		showInSearch, hiddenWidget int64
		w                          = Workflow{Shortcut: s}
	)
	err := l.db.QueryRow(loadQuery, s.pk).Scan(&actions, &inputs, &outputs, &questions,
		&w.ClientVersion, &w.MinimumClientVersion, &fallback, &inputVars, &showInSearch, &hiddenWidget)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("shortcut %q (%s) has no action data", s.Name, s.WorkflowID)
	}
	if err != nil {
		return nil, err
	}

	if w.Actions, err = decodePlistArray(actions); err != nil {
		return nil, fmt.Errorf("decoding actions for %q: %w", s.Name, err)
	}
	if w.InputContentItemClasses, err = decodePlistArray(inputs); err != nil {
		return nil, fmt.Errorf("decoding input classes for %q: %w", s.Name, err)
	}
	if w.OutputContentItemClasses, err = decodePlistArray(outputs); err != nil {
		return nil, fmt.Errorf("decoding output classes for %q: %w", s.Name, err)
	}
	if w.ImportQuestions, err = decodePlistArray(questions); err != nil {
		return nil, fmt.Errorf("decoding import questions for %q: %w", s.Name, err)
	}

	w.HasOutputFallback = fallback == 1
	w.HasShortcutInputVariables = inputVars == 1
	w.ShowInSearch = showInSearch == 1
	w.HiddenFromWidget = hiddenWidget == 1

	// ZACTIONCOUNT is not maintained for every row (it reads 0 for some shortcuts even
	// when actions exist), so prefer the real length now that we have decoded them.
	w.ActionCount = len(w.Actions)
	return &w, nil
}

// FindByName returns the shortcut with the given name, which Shortcuts does not
// guarantee to be unique.
func (l *Library) FindByName(name string) ([]Shortcut, error) {
	all, err := l.List()
	if err != nil {
		return nil, err
	}
	var out []Shortcut
	for _, s := range all {
		if s.Name == name {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no shortcut named %q", name)
	}
	return out, nil
}

func coreDataTime(v sql.NullFloat64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	sec, frac := int64(v.Float64), v.Float64-float64(int64(v.Float64))
	return coreDataEpoch.Add(time.Duration(sec)*time.Second +
		time.Duration(frac*float64(time.Second))).Local()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
