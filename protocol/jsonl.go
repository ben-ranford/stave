package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

var ErrBatch = errors.New("batch requests are not supported")

// DecodeLine validates exactly one bounded UTF-8 JSON-RPC request object.
func DecodeLine(line []byte, max int) (Request, error) {
	if max > 0 && len(line) > max {
		return Request{}, fmt.Errorf("message exceeds limit")
	}
	if !utf8.Valid(line) {
		return Request{}, errors.New("invalid UTF-8")
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return Request{}, errors.New("empty line")
	}
	if line[0] == '[' {
		return Request{}, ErrBatch
	}
	if line[0] != '{' {
		return Request{}, errors.New("request must be object")
	}
	if err := validateObject(line, 0); err != nil {
		return Request{}, err
	}
	var r Request
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Request{}, err
	}
	if err := ensureEOF(dec); err != nil {
		return Request{}, err
	}
	if r.JSONRPC != JSONRPC || r.Method == "" || (len(r.ID) > 0 && !ID(r.ID).Valid()) {
		return Request{}, errors.New("invalid JSON-RPC request")
	}
	return r, nil
}

func ensureEOF(dec *json.Decoder) error {
	var x any
	if err := dec.Decode(&x); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}

// validateObject recursively rejects duplicate keys and caps nesting depth.
func validateObject(b []byte, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting exceeds limit")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	t, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := t.(json.Delim)
	if !ok || d != '{' {
		return nil
	}
	seen := map[string]struct{}{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := kt.(string)
		if !ok {
			return errors.New("invalid object key")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = struct{}{}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		trim := bytes.TrimSpace(raw)
		if len(trim) > 0 && trim[0] == '{' {
			if err := validateObject(trim, depth+1); err != nil {
				return err
			}
		} else if len(trim) > 0 && trim[0] == '[' {
			if err := validateArray(trim, depth+1); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token()
	if err != nil {
		return err
	}
	return ensureEOF(dec)
}
func validateArray(b []byte, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting exceeds limit")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if t != json.Delim('[') {
		return nil
	}
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		trim := bytes.TrimSpace(raw)
		if len(trim) > 0 && trim[0] == '{' {
			if err := validateObject(trim, depth+1); err != nil {
				return err
			}
		} else if len(trim) > 0 && trim[0] == '[' {
			if err := validateArray(trim, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	return ensureEOF(dec)
}
