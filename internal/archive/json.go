package archive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

const (
	TypeKey    = "__type__"
	ValueKey   = "__value__"
	TypeData   = "data"
	TypeDate   = "date"
	jsonIndent = "  "
)

func CanonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", jsonIndent)
	if err := enc.Encode(Canonicalize(v)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WriteJSON(path string, wf map[string]any) (changed bool, err error) {
	data, err := CanonicalJSON(wf)
	if err != nil {
		return false, fmt.Errorf("encoding %s: %w", path, err)
	}
	return writeIfChanged(path, data)
}

func Canonicalize(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return taggedData(t)
	case time.Time:
		return map[string]any{TypeKey: TypeDate, ValueKey: t.Unix()}
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = Canonicalize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = Canonicalize(val)
		}
		return out
	case string, bool, float64, int, int32, int64:
		return t
	case uint64:
		return jsonInteger(t)
	default:
		return canonicalizeByKind(v)
	}
}

func canonicalizeByKind(v any) any {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return Canonicalize(rv.Elem().Interface())
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return fmt.Sprint(v)
		}
		out := make(map[string]any, rv.Len())
		for iter := rv.MapRange(); iter.Next(); {
			out[iter.Key().String()] = Canonicalize(iter.Value().Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, rv.Len())
			reflect.Copy(reflect.ValueOf(b), rv)
			return taggedData(b)
		}
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = Canonicalize(rv.Index(i).Interface())
		}
		return out
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return jsonInteger(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.String:
		return rv.String()
	case reflect.Struct:
		return canonicalizeStruct(v)
	default:
		return fmt.Sprint(v)
	}
}

func canonicalizeStruct(v any) any {
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return fmt.Sprint(v)
	}
	return decoded
}

func taggedData(b []byte) map[string]any {
	return map[string]any{TypeKey: TypeData, ValueKey: base64.StdEncoding.EncodeToString(b)}
}

func jsonInteger(u uint64) any {
	if u <= math.MaxInt64 {
		return int64(u)
	}
	return u
}

func writeIfChanged(path string, data []byte) (changed bool, err error) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return false, nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}
