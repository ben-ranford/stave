package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicAPIInventoryIncludesExportedTypeDefinitions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "api.go")
	write := func(source string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	render := func() string {
		t.Helper()
		got, err := renderPublicAPI("example.com/api", []goListPackage{{ImportPath: "example.com/api", Dir: dir, GoFiles: []string{"api.go"}}})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	write(`package api
type Report[T any] struct {
	Name string ` + "`json:\"name\"`" + `
	hidden int
}
type Runner[T any] interface { Run(T) error }
type Label = string
type ID string
`)
	before := render()
	for _, want := range []string{
		"type Report[T any] struct { Name string `json:\"name\"` }",
		"type Runner[T any] interface { Run(T) error }",
		"type Label = string",
		"type ID string",
	} {
		if !strings.Contains(before, want) {
			t.Fatalf("public API inventory missing %q:\n%s", want, before)
		}
	}
	if strings.Contains(before, "hidden") {
		t.Fatalf("public API inventory exposed unexported field:\n%s", before)
	}

	write(`package api
type Report[T any] struct { Value string }
type Runner[T any] interface { Run(T) error }
type Label = string
type ID string
`)
	after := render()
	if before == after {
		t.Fatal("changing an exported struct field did not change the public API inventory")
	}
	if !strings.Contains(after, "type Report[T any] struct { Value string }") {
		t.Fatalf("updated exported field missing from public API inventory:\n%s", after)
	}
}

func TestPublicAPIInventoryIncludesExportedValueSemantics(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "api.go")
	render := func(source string) string {
		t.Helper()
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := renderPublicAPI("example.com/api", []goListPackage{{ImportPath: "example.com/api", Dir: dir, GoFiles: []string{"api.go"}}})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	before := render(`package api
type State int

const (
	SchemaVersion = "v1"
	TypedLimit int64 = 2
	Initial State = iota
	Ready
	Complete = iota + 4
)

var (
	Limit int
	Inferred = 42
	Labels = map[string]int{}
)
`)
	for _, want := range []string{
		`const SchemaVersion untyped string = "v1"`,
		"const TypedLimit int64 = 2",
		"const Initial State = 2",
		"const Ready State = 3",
		"const Complete untyped int = 8",
		"var Inferred int",
		"var Labels map[string]int",
		"var Limit int",
	} {
		if !strings.Contains(before, want) {
			t.Fatalf("public API inventory missing %q:\n%s", want, before)
		}
	}

	after := render(`package api

type State int

const (
	SchemaVersion = "v2"
	TypedLimit int64 = 3
	Initial State = iota
	Ready
	Complete = iota + 4
)

var (
	Limit string
	Inferred = "42"
	Labels = map[string]string{}
)
`)
	if before == after {
		t.Fatal("incompatible exported variable types and constant values produced identical API inventory")
	}

	formatted := render(`package api
type State int
const(SchemaVersion="v1";TypedLimit int64=2;Initial State=iota;Ready;Complete=iota+4)
var(Limit int;Inferred=42;Labels=map[string]int{})
`)
	if before != formatted {
		t.Fatalf("formatting-only changes altered the public API inventory:\nbefore:\n%s\nformatted:\n%s", before, formatted)
	}
	if strings.Index(before, "var Inferred int") > strings.Index(before, "var Limit int") {
		t.Fatalf("public API entries are not sorted:\n%s", before)
	}
}
