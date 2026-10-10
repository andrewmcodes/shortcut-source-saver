package archive

import (
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

type Metadata struct {
	Name                 string   `json:"name"`
	WorkflowID           string   `json:"workflow_id"`
	ActionCount          int      `json:"action_count"`
	Created              string   `json:"created"`
	Modified             string   `json:"modified"`
	Glyph                int64    `json:"glyph"`
	Color                string   `json:"color"`
	ClientVersion        string   `json:"client_version"`
	MinimumClientVersion string   `json:"minimum_client_version"`
	WorkflowTypes        []string `json:"workflow_types"`
	Status               string   `json:"status,omitempty"`
	ConflictOf           *Origin  `json:"conflict_of,omitempty"`
}

type Origin struct {
	WorkflowID string `json:"workflow_id"`
	Name       string `json:"name"`
}

func NewMetadata(w *shortcuts.Workflow) Metadata {
	return Metadata{
		Name:                 w.Name,
		WorkflowID:           w.WorkflowID,
		ActionCount:          w.ActionCount,
		Created:              rfc3339OrEmpty(w.Created),
		Modified:             rfc3339OrEmpty(w.Modified),
		Glyph:                w.Icon.Glyph,
		Color:                HexColor(w.Icon.Color),
		ClientVersion:        w.ClientVersion,
		MinimumClientVersion: w.MinimumClientVersion,
		WorkflowTypes:        WorkflowTypes(w),
		Status:               string(w.Status),
		ConflictOf:           newOrigin(w.ConflictOf),
	}
}

func newOrigin(original shortcuts.Original) *Origin {
	if original == (shortcuts.Original{}) {
		return nil
	}
	return &Origin{WorkflowID: original.WorkflowID, Name: original.Name}
}

func (m Metadata) ShortcutStatus() shortcuts.Status {
	return shortcuts.Status(m.Status)
}

func (m Metadata) Original() shortcuts.Original {
	if m.ConflictOf == nil {
		return shortcuts.Original{}
	}
	return shortcuts.Original{WorkflowID: m.ConflictOf.WorkflowID, Name: m.ConflictOf.Name}
}

func WriteMetadata(path string, m Metadata) error {
	data, err := CanonicalJSON(m)
	if err != nil {
		return err
	}
	_, err = writeIfChanged(path, data)
	return err
}

func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
