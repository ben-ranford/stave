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
type Runner[T any] interface { Run(value T) error; private() }
type Label = string
type ID string
`)
	before := render()
	for _, want := range []string{
		"type Report[T any] struct { Name string `json:\"name\"` }",
		"type Runner[T any] interface { Run(T) error; private() }",
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

func TestPublicAPIInventoryNormalizesParameterNamesAndInterfaceOrder(t *testing.T) {
	dir := t.TempDir()
	render := func(source string) string {
		t.Helper()
		path := filepath.Join(dir, "api.go")
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := renderPublicAPI("example.com/api", []goListPackage{{ImportPath: "example.com/api", Dir: dir, GoFiles: []string{"api.go"}}})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	before := render("package api\ntype Contract interface { Zebra(value string) error; Alpha() }\nfunc Keep(value string) string { return value }\n")
	after := render("package api\ntype Contract interface { Alpha(); Zebra(input string) error }\nfunc Keep(input string) string { return input }\n")
	if before != after {
		t.Fatalf("semantic-equivalent declarations changed inventory:\n%s\n%s", before, after)
	}
	grouped := render("package api\nfunc Keep(first, second string) (left, right string) { return first, second }\n")
	ungrouped := render("package api\nfunc Keep(string, string) (string, string) { return \"\", \"\" }\n")
	if grouped != ungrouped {
		t.Fatalf("grouped parameters or results changed inventory:\n%s\n%s", grouped, ungrouped)
	}
	changedArity := render("package api\nfunc Keep(string, string) string { return \"\" }\n")
	if grouped == changedArity {
		t.Fatalf("result arity change did not change inventory:\n%s", grouped)
	}
	nestedBefore := render("package api\nfunc Keep(callback func(value string) string) {}\n")
	nestedAfter := render("package api\nfunc Keep(callback func(input string) string) {}\n")
	if nestedBefore != nestedAfter {
		t.Fatalf("nested function parameter names changed inventory:\n%s\n%s", nestedBefore, nestedAfter)
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
	for _, want := range []string{`const SchemaVersion untyped string = "v2"`, "const TypedLimit int64 = 3"} {
		if !strings.Contains(after, want) {
			t.Fatalf("mutated API inventory missing constant value %q:\n%s", want, after)
		}
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

func TestPublicAPIInventoryResolvesInModuleDependencyValues(t *testing.T) {
	t.Parallel()
	consumer, dependency := t.TempDir(), t.TempDir()
	for path, source := range map[string]string{
		filepath.Join(consumer, "api.go"): `package api
import "example.com/api/dep"
var Selected = dep.Ready
const Initial = dep.Ready
`,
		filepath.Join(dependency, "dep.go"): `package dep
type State int
const Ready State = 7
`,
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := renderPublicAPI("example.com/api", []goListPackage{
		{ImportPath: "example.com/api", Dir: consumer, GoFiles: []string{"api.go"}},
		{ImportPath: "example.com/api/dep", Dir: dependency, GoFiles: []string{"dep.go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"var Selected example.com/api/dep.State", "const Initial example.com/api/dep.State = 7"} {
		if !strings.Contains(inventory, want) {
			t.Fatalf("dependency-defined value missing %q:\n%s", want, inventory)
		}
	}
}
