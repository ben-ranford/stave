package main

import (
	"context"
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

func TestPublicAPIInventoryIncludesAliasReachableHiddenTypes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.go")
	render := func(source string) string {
		t.Helper()
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		inventory, err := renderPublicAPI("example.com/api", []goListPackage{{ImportPath: "example.com/api", Dir: dir, GoFiles: []string{"api.go"}}})
		if err != nil {
			t.Fatal(err)
		}
		return inventory
	}

	before := render(`package api
type hidden struct { Value string }
type Public = hidden
type Wrapped = []hidden
type Again = hidden
type recursive struct { Next *recursive; Value string }
type Recursive = recursive
`)
	for _, want := range []string{
		"type Public = hidden",
		"type Wrapped = []hidden",
		"type hidden struct { Value string }",
		"type recursive struct { Next *recursive; Value string }",
	} {
		if !strings.Contains(before, want) {
			t.Fatalf("public alias inventory missing %q:\n%s", want, before)
		}
	}
	if strings.Count(before, "type hidden struct") != 1 {
		t.Fatalf("repeated aliases duplicated hidden definition:\n%s", before)
	}
	if strings.Count(before, "type recursive struct") != 1 {
		t.Fatalf("recursive hidden definition was duplicated:\n%s", before)
	}

	fieldChanged := render(`package api
type hidden struct { Value int }
type Public = hidden
type Wrapped = []hidden
type Again = hidden
type recursive struct { Next *recursive; Value string }
type Recursive = recursive
`)
	if before == fieldChanged {
		t.Fatalf("hidden field change reachable through exported alias did not change inventory:\n%s", before)
	}
	nominalBefore := render(`package api
type hiddenOne struct { Value string }
type Public = hiddenOne
`)
	nominallyChanged := render(`package api
type hiddenTwo struct { Value string }
type Public = hiddenTwo
	`)
	if nominalBefore == nominallyChanged {
		t.Fatalf("same-underlying hidden type replacement did not change inventory:\n%s", nominalBefore)
	}
}

func TestPublicAPIInventoryIncludesHiddenTypesReachableFromPublicDeclarations(t *testing.T) {
	for name, source := range map[string][2]string{
		"field": {
			"type hidden string\ntype Public struct { Value hidden }",
			"type hidden int\ntype Public struct { Value hidden }",
		},
		"function": {
			"type hidden string\nfunc Convert(value hidden) hidden { return value }",
			"type hidden int\nfunc Convert(value hidden) hidden { return value }",
		},
		"inferred variable": {
			"type hidden string\nvar Default = hidden(\"default\")",
			"type hidden int\nvar Default = hidden(1)",
		},
		"typed variable": {
			"type hidden string\nvar Default hidden",
			"type hidden int\nvar Default hidden",
		},
		"named type": {
			"type hidden struct { Value string }\ntype Public hidden",
			"type hidden struct { Value int }\ntype Public hidden",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "api.go")
			render := func(declarations string) string {
				t.Helper()
				if err := os.WriteFile(path, []byte("package api\n"+declarations+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				inventory, err := renderPublicAPI("example.com/api", []goListPackage{{ImportPath: "example.com/api", Dir: dir, GoFiles: []string{"api.go"}}})
				if err != nil {
					t.Fatal(err)
				}
				return inventory
			}
			before := render(source[0])
			after := render(source[1])
			if before == after || !strings.Contains(before, "type hidden") || !strings.Contains(after, "type hidden") {
				t.Fatalf("reachable hidden type change did not alter inventory:\n%s\n%s", before, after)
			}
		})
	}
}

func TestPublicAPIInventoryRecordsStructComparabilityWithoutPrivateFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.go")
	render := func(source string) string {
		t.Helper()
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		inventory, err := renderPublicAPI("example.com/api", []goListPackage{{ImportPath: "example.com/api", Dir: dir, GoFiles: []string{"api.go"}}})
		if err != nil {
			t.Fatal(err)
		}
		return inventory
	}

	before := render(`package api
type keyBody struct { ID int; private int }
type Key keyBody
type Labels struct { Values []string }
`)
	for _, want := range []string{"struct-comparable Key true", "struct-comparable Labels false"} {
		if !strings.Contains(before, want) {
			t.Fatalf("comparability marker missing %q:\n%s", want, before)
		}
	}
	if strings.Contains(before, "private") {
		t.Fatalf("private field leaked into inventory:\n%s", before)
	}

	after := render(`package api
type keyBody struct { ID int; private []int }
type Key keyBody
type Labels struct { Values []string }
`)
	if before == after || !strings.Contains(after, "struct-comparable Key false") {
		t.Fatalf("private-field comparability change did not alter inventory:\n%s\n%s", before, after)
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
	nestedResultBefore := render("package api\nfunc Keep() func(value string) string { return nil }\n")
	nestedResultAfter := render("package api\nfunc Keep() func(input string) string { return nil }\n")
	if nestedResultBefore != nestedResultAfter {
		t.Fatalf("nested function result parameter names changed inventory:\n%s\n%s", nestedResultBefore, nestedResultAfter)
	}
}

func TestPublicAPIInventoryNormalizesNestedFunctionParameterNames(t *testing.T) {
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
	for name, declarations := range map[string][2]string{
		"slice":   {"func Keep(value []func(value string) string) {}", "func Keep(input []func(input string) string) {}"},
		"array":   {"func Keep(value [1]func(value string) string) {}", "func Keep(input [1]func(input string) string) {}"},
		"map":     {"func Keep(value map[string]func(value string) string) {}", "func Keep(input map[string]func(input string) string) {}"},
		"channel": {"func Keep(value chan func(value string) string) {}", "func Keep(input chan func(input string) string) {}"},
		"generic": {"type Box[T any] struct{}\nfunc Keep(value Box[func(value string) string]) {}", "type Box[T any] struct{}\nfunc Keep(input Box[func(input string) string]) {}"},
	} {
		t.Run(name, func(t *testing.T) {
			before := render("package api\n" + declarations[0] + "\n")
			after := render("package api\n" + declarations[1] + "\n")
			if before != after {
				t.Fatalf("nested function parameter rename changed inventory:\n%s\n%s", before, after)
			}
		})
	}
}

func TestPublicAPIInventoryNormalizesAnonymousTypeParameterNames(t *testing.T) {
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
	for name, declarations := range map[string][2]string{
		"struct": {
			"func Keep(value struct { Callback func(value string) string }) {}",
			"func Keep(value struct { Callback func(input string) string }) {}",
		},
		"interface": {
			"func Keep(value interface { Zebra(func(value string) string); Alpha() }) {}",
			"func Keep(value interface { Alpha(); Zebra(func(input string) string) }) {}",
		},
		"constraint": {
			"func Keep[T interface { ~func(value string) string }](value T) {}",
			"func Keep[T interface { ~func(input string) string }](value T) {}",
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := render("package api\n" + declarations[0] + "\n")
			after := render("package api\n" + declarations[1] + "\n")
			if before != after {
				t.Fatalf("anonymous %s parameter rename changed inventory:\n%s\n%s", name, before, after)
			}
		})
	}
}

func TestPublicAPIInventoryPreservesAnonymousStructFieldsTagsAndEmbeddings(t *testing.T) {
	dir := t.TempDir()
	source := "package api\ntype Embedded struct{}\nfunc Keep(value struct { Embedded; private string `json:\"private\"`; Callback func(value string) string `json:\"callback\"` }) {}\n"
	if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := renderPublicAPI("example.com/api", []goListPackage{{ImportPath: "example.com/api", Dir: dir, GoFiles: []string{"api.go"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `func Keep(struct{Embedded; private string "json:\"private\""; Callback func(string) string "json:\"callback\""})`
	if !strings.Contains(inventory, want) {
		t.Fatalf("anonymous struct details were not preserved:\n%s", inventory)
	}
}

func TestPublicAPIInventoryParenthesizesReceiveOnlyChannelElement(t *testing.T) {
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

	receiveElement := render("package api\nfunc Keep(value chan (<-chan int)) {}\n")
	if !strings.Contains(receiveElement, "func Keep(chan (<-chan int))") {
		t.Fatalf("receive-only channel element was not parenthesized:\n%s", receiveElement)
	}
	sendOuter := render("package api\nfunc Keep(value chan<- chan int) {}\n")
	if receiveElement == sendOuter {
		t.Fatalf("distinct nested channel types produced identical inventory:\n%s", receiveElement)
	}
}

func TestPublicAPIInventorySelectsRequestedBuildTarget(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/api\n\ngo 1.22\n")
	write("api.go", "package api\ntype Common struct{}\n")
	write("api_linux.go", "//go:build linux\n\npackage api\ntype LinuxOnly struct{}\n")
	write("api_windows.go", "//go:build windows\n\npackage api\n\nimport \"syscall\"\n\nvar _ = syscall.UTF16FromString\ntype WindowsOnly struct{}\n")
	for _, target := range []struct {
		goos string
		want string
		omit string
	}{
		{goos: "linux", want: "type LinuxOnly struct {  }", omit: "WindowsOnly"},
		{goos: "windows", want: "type WindowsOnly struct {  }", omit: "LinuxOnly"},
	} {
		t.Run(target.goos, func(t *testing.T) {
			pkgs, err := listPackagesInDirForTarget(context.Background(), dir, false, target.goos, "amd64")
			if err != nil {
				t.Fatal(err)
			}
			inventory, err := renderPublicAPIForTarget(context.Background(), "example.com/api", pkgs, target.goos, "amd64")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(inventory, target.want) || strings.Contains(inventory, target.omit) {
				t.Fatalf("target %s inventory selected wrong files:\n%s", target.goos, inventory)
			}
		})
	}
}

func TestPublicAPIInventoryPreservesImportedTypePackageIdentity(t *testing.T) {
	dir := t.TempDir()
	write := func(relative, source string) {
		t.Helper()
		path := filepath.Join(dir, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("one/model.go", "package model\ntype ID string\ntype Constraint interface { ~string }\n")
	write("two/model.go", "package model\ntype ID string\ntype Constraint interface { ~string }\n")
	apiDir := filepath.Join(dir, "api")
	render := func(path, alias, declaration string) string {
		t.Helper()
		source := strings.ReplaceAll(declaration, "$ID", alias+".ID")
		source = strings.ReplaceAll(source, "$Constraint", alias+".Constraint")
		write("api/api.go", "package api\nimport "+alias+" \""+path+"\"\n"+source)
		modelDir := filepath.Join(dir, map[string]string{"example.com/model/one": "one", "example.com/model/two": "two"}[path])
		inventory, err := renderPublicAPI("example.com/api", []goListPackage{
			{ImportPath: "example.com/api", Dir: apiDir, GoFiles: []string{"api.go"}},
			{ImportPath: path, Dir: modelDir, GoFiles: []string{"model.go"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return inventory
	}
	for name, declaration := range map[string]string{
		"function":         "func Keep(value $ID) $ID { return value }\n",
		"interface":        "type Contract interface { Keep($ID) $ID }\n",
		"struct":           "type Record struct { ID $ID }\n",
		"anonymous-struct": "func Keep(value struct { id $ID }) {}\n",
		"variable":         "var Default $ID\n",
		"constraint":       "type Holder[T $Constraint] struct { Value T }\n",
	} {
		t.Run(name, func(t *testing.T) {
			one := render("example.com/model/one", "model", declaration)
			two := render("example.com/model/two", "model", declaration)
			if one == two {
				t.Fatalf("imported type package change did not alter inventory:\n%s", one)
			}
			if equivalent := render("example.com/model/one", "identity", declaration); one != equivalent {
				t.Fatalf("import alias changed inventory:\n%s\n%s", one, equivalent)
			}
		})
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

func TestPublicAPIInventoryNormalizesExportedFunctionValueParameterNames(t *testing.T) {
	dir := t.TempDir()
	render := func(source string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		inventory, err := renderPublicAPI("example.com/api", []goListPackage{{ImportPath: "example.com/api", Dir: dir, GoFiles: []string{"api.go"}}})
		if err != nil {
			t.Fatal(err)
		}
		return inventory
	}

	before := render("package api\nvar Hook func(value string) func(result string) error\n")
	after := render("package api\nvar Hook func(input string) func(output string) error\n")
	if before != after {
		t.Fatalf("exported function value parameter names changed inventory:\n%s\n%s", before, after)
	}
}
