package shortcuts

import (
	"fmt"

	"howett.net/plist"
)

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
