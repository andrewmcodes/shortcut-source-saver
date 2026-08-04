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

var coreDataEpoch = time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)

var ErrNoAccess = errors.New("cannot read the Shortcuts database")

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

type Icon struct {
	Glyph int64
	Color uint32
}

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

type Library struct {
	db      *sql.DB
	tempDir string
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Shortcuts", "Shortcuts.sqlite"), nil
}

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
	for _, suffix := range walSidecarSuffixes {
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

var walSidecarSuffixes = []string{"-wal", "-shm"}

func (l *Library) Close() error {
	err := l.db.Close()
	if rmErr := os.RemoveAll(l.tempDir); err == nil {
		err = rmErr
	}
	return err
}

var requiredSchema = map[string][]string{
	"ZSHORTCUT":        {"ZNAME", "ZWORKFLOWID", "ZACTIONS", "ZICON", "ZTOMBSTONED"},
	"ZSHORTCUTACTIONS": {"ZDATA"},
	"ZSHORTCUTICON":    {"ZGLYPHNUMBER", "ZBACKGROUNDCOLORVALUE"},
}

func (l *Library) verifySchema() error {
	const unsupported = "this macOS version stores Shortcuts differently than this tool understands"

	for table, columns := range requiredSchema {
		rows, err := l.db.Query("SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			return err
		}
		present := map[string]bool{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			present[name] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(present) == 0 {
			return fmt.Errorf("table %s is missing: %s", table, unsupported)
		}
		for _, c := range columns {
			if !present[c] {
				return fmt.Errorf("column %s.%s is missing: %s", table, c, unsupported)
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

func (l *Library) List() ([]Shortcut, error) {
	rows, err := l.db.Query(listQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Shortcut
	for rows.Next() {
		var (
			s                 Shortcut
			created, modified sql.NullFloat64
			count             sql.NullInt64
			color             int64
			hidden            int64
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

func (l *Library) Load(s Shortcut) (*Workflow, error) {
	var (
		actions                    []byte
		inputs, outputs, questions []byte
		fallback, inputVars        int64
		showInSearch, hiddenWidget int64
	)
	w := Workflow{Shortcut: s}

	err := l.db.QueryRow(loadQuery, s.pk).Scan(&actions, &inputs, &outputs, &questions,
		&w.ClientVersion, &w.MinimumClientVersion, &fallback, &inputVars, &showInSearch, &hiddenWidget)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("shortcut %q (%s) has no action data", s.Name, s.WorkflowID)
	}
	if err != nil {
		return nil, err
	}

	for _, field := range []struct {
		name string
		data []byte
		dst  *[]any
	}{
		{"actions", actions, &w.Actions},
		{"input classes", inputs, &w.InputContentItemClasses},
		{"output classes", outputs, &w.OutputContentItemClasses},
		{"import questions", questions, &w.ImportQuestions},
	} {
		if *field.dst, err = decodePlistArray(field.data); err != nil {
			return nil, fmt.Errorf("decoding %s for %q: %w", field.name, s.Name, err)
		}
	}

	w.HasOutputFallback = fallback == 1
	w.HasShortcutInputVariables = inputVars == 1
	w.ShowInSearch = showInSearch == 1
	w.HiddenFromWidget = hiddenWidget == 1
	w.ActionCount = len(w.Actions)

	return &w, nil
}

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
	whole := int64(v.Float64)
	frac := v.Float64 - float64(whole)
	return coreDataEpoch.Add(time.Duration(whole)*time.Second +
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
