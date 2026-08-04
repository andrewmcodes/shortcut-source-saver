package shortcuts

import (
	"fmt"

	"howett.net/plist"
)

// decodePlistArray decodes one of the binary-plist blobs the Shortcuts database stores in
// place of a plist array. Empty and NULL columns decode to an empty slice so that callers
// can write them straight back out as `[]`.
func decodePlistArray(data []byte) ([]any, error) {
	if len(data) == 0 {
		return []any{}, nil
	}
	var v any
	if _, err := plist.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a plist array, got %T", v)
	}
	if items == nil {
		items = []any{}
	}
	return items, nil
}
