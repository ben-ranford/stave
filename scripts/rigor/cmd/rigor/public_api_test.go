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
