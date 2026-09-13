package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

const JSONPolicyVersion = "stave-json-canonical-v1:use-number-decimal"

func Encode(v any) ([]byte, error) {
	if err := valid(v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// Decode applies the canonical decoder's number-preservation policy. Callers
// retain ownership of validation, trailing-value handling, and error context.
func Decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func Hash(v any) ([32]byte, error) {
	b, e := Encode(v)
	if e != nil {
		return [32]byte{}, e
	}
	return sha256.Sum256(b), nil
}
func valid(v any) error { rv := reflect.ValueOf(v); return walk(rv) }
func walk(v reflect.Value) error {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return walk(v.Elem())
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return fmt.Errorf("invalid UTF-8")
		}
	case reflect.Map:
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
		for _, k := range keys {
			if err := walk(k); err != nil {
				return err
			}
			if err := walk(v.MapIndex(k)); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := walk(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath != "" {
				continue
			}
			if err := walk(v.Field(i)); err != nil {
				return err
			}
		}
	}
	return nil
}
func Equal(a, b any) bool { x, _ := Encode(a); y, _ := Encode(b); return bytes.Equal(x, y) }
func JSON(raw []byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	v, e := normalizeJSON(v)
	if e != nil {
		return nil, e
	}
	return Encode(v)
}
func normalizeJSON(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		n, e := normalizeNumber(x.String())
		if e != nil {
			return nil, e
		}
		return json.Number(n), nil
	case []any:
		for i, z := range x {
			q, e := normalizeJSON(z)
			if e != nil {
				return nil, e
			}
			x[i] = q
		}
	case map[string]any:
		for k, z := range x {
			q, e := normalizeJSON(z)
			if e != nil {
				return nil, e
			}
			x[k] = q
		}
	}
	return v, nil
}
func normalizeNumber(s string) (string, error) {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign = "-"
		s = s[1:]
	}
	exp := new(big.Int)
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		if _, ok := exp.SetString(s[i+1:], 10); !ok {
			return "", fmt.Errorf("invalid exponent")
		}
		s = s[:i]
	}
	frac := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		frac = len(s) - i - 1
		s = s[:i] + s[i+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" || strings.Trim(s, "0") == "" {
		return "0", nil
	}
	n := len(s)
	s = strings.TrimRight(s, "0")
	exp.Add(exp, big.NewInt(int64(n-len(s))))
	exp.Sub(exp, big.NewInt(int64(frac)))
	return sign + s + "e" + exp.String(), nil
}
