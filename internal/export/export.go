package export

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/backup"
	"github.com/andrewmcodes/shortcut-source-saver/internal/scan"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

var ErrOutputExists = errors.New("output already exists")

type Source interface {
	List() ([]shortcuts.Shortcut, error)
	Load(shortcuts.Shortcut) (*shortcuts.Workflow, error)
}

type Options struct {
	IncludeHidden bool
	AllowSecrets  bool
}

type File struct {
	Path     string
	Data     []byte
	Modified time.Time
}

type Failure struct {
	Shortcut shortcuts.Shortcut
	Err      error
}

type CollectError struct {
	Failures []Failure
}

func (e *CollectError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d shortcut(s) could not be exported; nothing was written:", len(e.Failures))
	for _, f := range e.Failures {
		fmt.Fprintf(&b, "\n  %s (%s): %v", f.Shortcut.Name, f.Shortcut.WorkflowID, f.Err)
	}
	return b.String()
}

func Collect(src Source, opts Options) ([]File, int, error) {
	all, err := src.List()
	if err != nil {
		return nil, 0, err
	}
	slugs := backup.ResolveSlugs(all)

	var (
		files    []File
		failures []Failure
		count    int
	)
	for _, sc := range all {
		if sc.Hidden && !opts.IncludeHidden {
			continue
		}
		entries, err := filesFor(src, sc, slugs[sc.WorkflowID], opts)
		if err != nil {
			failures = append(failures, Failure{Shortcut: sc, Err: err})
			continue
		}
		files = append(files, entries...)
		count++
	}
	if len(failures) > 0 {
		return nil, 0, &CollectError{Failures: failures}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, count, nil
}

func filesFor(src Source, sc shortcuts.Shortcut, slug string, opts Options) ([]File, error) {
	w, err := src.Load(sc)
	if err != nil {
		return nil, err
	}
	if findings := scan.Scan(w.Actions); len(findings) > 0 && !opts.AllowSecrets {
		return nil, fmt.Errorf("%w; re-run with --allow-secrets to override:\n%s",
			backup.ErrSecretsFound, scan.Summary(findings))
	}

	source, err := archive.CanonicalJSON(archive.BuildWorkflow(w))
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", backup.SourceFileName, err)
	}
	metadata, err := archive.CanonicalJSON(archive.NewMetadata(w))
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", backup.MetadataFileName, err)
	}

	dir := path.Join(backup.ShortcutsDir, slug)
	return []File{
		{Path: path.Join(dir, backup.SourceFileName), Data: source, Modified: w.Modified},
		{Path: path.Join(dir, backup.MetadataFileName), Data: metadata, Modified: w.Modified},
	}, nil
}

func WriteZip(w io.Writer, files []File) error {
	zw := zip.NewWriter(w)
	for _, f := range files {
		header := &zip.FileHeader{Name: f.Path, Method: zip.Deflate}
		if !f.Modified.IsZero() {
			header.Modified = f.Modified
		}
		entry, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("adding %s: %w", f.Path, err)
		}
		if _, err := entry.Write(f.Data); err != nil {
			return fmt.Errorf("adding %s: %w", f.Path, err)
		}
	}
	return zw.Close()
}

func CreateExclusive(target string, write func(io.Writer) error) (err error) {
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%w: %s", ErrOutputExists, target)
	}
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(target)
		}
	}()

	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}
