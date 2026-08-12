package tokens

import (
	"encoding/hex"
	"sort"

	"github.com/ben-ranford/stave/internal/canonical"
)

type Value struct {
	Name  string
	Value any
}

func Stable(values map[string]any) []Value {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Value, 0, len(keys))
	for _, k := range keys {
		out = append(out, Value{Name: k, Value: values[k]})
	}
	return out
}

func CanonicalJSON(v any) ([]byte, error) {
	return canonical.Encode(v)
}

func CanonicalHash(v any) ([32]byte, error) {
	return canonical.Hash(v)
}

func CanonicalHashString(v any) (string, error) {
	sum, err := CanonicalHash(v)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}
