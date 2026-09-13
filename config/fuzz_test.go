package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

const maxConfigFuzzBytes = 64 << 10

func FuzzConfigParseCanonical(f *testing.F) {
	for _, seed := range configFuzzSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if !configFuzzInputInBounds(input) {
			return
		}
		config, err := Parse(input)
		if err != nil {
			assertConfigParseErrorSafe(t, err)
			return
		}
		assertConfigCanonicalRoundTrip(t, config)
	})
}

func configFuzzInputInBounds(input []byte) bool {
	if len(input) > maxConfigFuzzBytes {
		return false
	}
	depth, valid := jsonNesting(input)
	return !valid || depth <= 64
}

func assertConfigParseErrorSafe(t *testing.T, err error) {
	t.Helper()
	if strings.Contains(err.Error(), "fuzz-secret") {
		t.Fatalf("config parser echoed a secret: %v", err)
	}
}

func assertConfigCanonicalRoundTrip(t *testing.T, config Config) {
	t.Helper()
	canonical := CanonicalJSON(config)
	roundTrip, err := decodeCanonicalConfig(canonical)
	if err != nil {
		t.Fatalf("accepted config did not round trip: %v", err)
	}
	if !bytes.Equal(canonical, CanonicalJSON(roundTrip)) {
		t.Fatalf("accepted config canonical encoding was unstable")
	}
	if HashString(config) != HashString(roundTrip) {
		t.Fatalf("accepted config canonical hash was unstable")
	}
}

// decodeCanonicalConfig decodes a resolved Config. Parse consumes sparse
// layers and applies Defaults, so it is intentionally not this decoder.
func decodeCanonicalConfig(data []byte) (Config, error) {
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("canonical config has trailing JSON")
	}
	if err := Validate(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func configFuzzSeeds() [][]byte {
	valid := []byte(`{"schemaVersion":"stave.config/v1","theme":{"mode":"dark"},"protocol":{"maxMessageBytes":4194304}}`)
	nested := []byte(`{"schemaVersion":"stave.config/v1","theme":{"mode":{"nested":true}}}`)
	limit := append([]byte(`{"schemaVersion":"stave.config/v1"}`), bytes.Repeat([]byte(" "), maxConfigFuzzBytes-len(`{"schemaVersion":"stave.config/v1"}`))...)
	return [][]byte{
		valid,
		[]byte(`{"schemaVersion":"stave.config/v1","theme":{"mode":"dark","unknown":true}}`),
		[]byte(`{"schemaVersion":"stave.config/v1","schemaVersion":"stave.config/v1"}`),
		[]byte(`{"schemaVersion":"stave.config/v1"} {}`),
		[]byte(`null`),
		[]byte(`{"schemaVersion":"stave.config/v1","app":{"id":"fuzz-secret"},"unknown":true}`),
		[]byte(`{"schemaVersion":"stave.config/v1","protocol":{"enabled":false}}`),
		[]byte(`{"schemaVersion":"stave.config/v1","runtime":{"restoreOnPanic":false}}`),
		[]byte(`{"schemaVersion":"stave.config/v1","app":{"id":"brace { in a string }"}}`),
		[]byte("\xff{"),
		nested,
		limit,
		append(append([]byte(nil), limit...), 'x'),
	}
}

// jsonNesting counts JSON delimiters through tokens so braces in strings do
// not exclude valid seeds from the bounded fuzz domain.
func jsonNesting(input []byte) (maximum int, valid bool) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return maximum, true
		}
		if err != nil {
			return 0, false
		}
		switch delimiter := token.(type) {
		case json.Delim:
			switch delimiter {
			case '{', '[':
				depth++
				if depth > maximum {
					maximum = depth
				}
			case '}', ']':
				depth--
			}
		}
	}
}
