package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareInventoriesRejectsBreakingChanges(t *testing.T) {
	baseline := `# Public API inventory
module example.com/api

[example.com/api]
func Keep(value string) string
method (Contract) Keep() string
type Contract interface { Keep() string }
type Options struct { Name string }
var Limit int
`
	tests := map[string]string{
		"removal": `# Public API inventory
module example.com/api

[example.com/api]
method (Contract) Keep() string
type Contract interface { Keep() string }
type Options struct { Name string }
var Limit int
`,
		"signature change":          strings.Replace(baseline, "func Keep(value string) string", "func Keep(value int) string", 1),
		"interface method addition": strings.Replace(baseline, "type Contract interface { Keep() string }", "type Contract interface { Keep() string; Extra() }", 1),
		"variable type change":      strings.Replace(baseline, "var Limit int", "var Limit string", 1),
	}
	for name, candidate := range tests {
		t.Run(name, func(t *testing.T) {
			if err := compareInventories(baseline, candidate); err == nil {
				t.Fatal("breaking API change was accepted")
			}
		})
	}
}

func TestCompareInventoriesAllowsAdditiveDeclarationsAndStructFields(t *testing.T) {
	baseline := `# Public API inventory
module example.com/api

[example.com/api]
func Keep(value string) string
type Options struct { Name string }
`
	candidate := `# Public API inventory
module example.com/api

[example.com/api]
func Extra() error
func Keep(value string) string
type Options struct { Enabled bool; Name string }
`
	if err := compareInventories(baseline, candidate); err != nil {
		t.Fatalf("additive API change was rejected: %v", err)
	}
}

func TestCompareInventoriesRejectsStructFieldReordering(t *testing.T) {
	baseline := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Pair struct { Left string; Right string }\n"
	reordered := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Pair struct { Right string; Left string }\n"
	if err := compareInventories(baseline, reordered); err == nil {
		t.Fatal("reordered exported fields were accepted")
	}
	withAddition := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Pair struct { Left string; Enabled bool; Right string }\n"
	if err := compareInventories(baseline, withAddition); err != nil {
		t.Fatalf("additive exported field was rejected: %v", err)
	}
	grouped := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Pair struct { Left, Right string }\n"
	if err := compareInventories(grouped, baseline); err != nil {
		t.Fatalf("grouped equivalent exported fields were rejected: %v", err)
	}
}

func TestCompareInventoriesPreservesStructComparability(t *testing.T) {
	baseline := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Key struct { ID int }\nstruct-comparable Key true\n"
	withNonComparableField := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Key struct { ID int; Values []string }\nstruct-comparable Key false\n"
	if err := compareInventories(baseline, withNonComparableField); err == nil {
		t.Fatal("non-comparable additive field was accepted")
	}
	privateFieldChanged := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Key struct { ID int }\nstruct-comparable Key false\n"
	if err := compareInventories(baseline, privateFieldChanged); err == nil {
		t.Fatal("private-field comparability change was accepted")
	}
	becameComparable := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Key struct { ID int }\nstruct-comparable Key true\n"
	wasNonComparable := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Key struct { ID int }\nstruct-comparable Key false\n"
	if err := compareInventories(wasNonComparable, becameComparable); err != nil {
		t.Fatalf("comparability strengthening was rejected: %v", err)
	}
}

func TestCompareInventoriesRejectsNestedStructFieldChanges(t *testing.T) {
	baseline := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Options struct { F struct { X int; Y int } }\n"
	candidate := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Options struct { F struct { X int; Z bool; Y int } }\n"
	if err := compareInventories(baseline, candidate); err == nil {
		t.Fatal("nested struct field change was accepted")
	}
}

func TestCompareInventoriesRejectsDeletedEmptyPackageAndAllowsFirstField(t *testing.T) {
	baseline := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Empty struct { }\n\n[example.com/api/empty]\n"
	if err := compareInventories(baseline, "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Empty struct { Enabled bool }\n"); err == nil {
		t.Fatal("deleted empty package accepted")
	}
	candidate := "# Public API inventory\nmodule example.com/api\n\n[example.com/api]\ntype Empty struct { Enabled bool }\n\n[example.com/api/empty]\n"
	if err := compareInventories(baseline, candidate); err != nil {
		t.Fatalf("first keyed field rejected: %v", err)
	}
}

func TestReadGoFloorAcceptsInlineCommentAndRejectsMissingDirective(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/api\n\ngo 1.22 // minimum supported Go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readGoFloor(dir); err != nil || got != "1.22" {
		t.Fatalf("floor=%q err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGoFloor(dir); err == nil {
		t.Fatal("missing Go floor accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/api\n\ngo 1.22 extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGoFloor(dir); err == nil {
		t.Fatal("malformed Go floor accepted")
	}
}

func TestPrereleaseV1TagRejectsLeadingZeroNumericIdentifiers(t *testing.T) {
	for _, tag := range []string{"v1.2.3-01", "v1.2.3-rc.01"} {
		if prereleaseV1Tag.MatchString(tag) {
			t.Fatalf("invalid prerelease %q accepted", tag)
		}
	}
	for _, tag := range []string{"v1.2.3-0", "v1.2.3-rc.1", "v1.2.3-rc.1+build.2"} {
		if !prereleaseV1Tag.MatchString(tag) {
			t.Fatalf("valid prerelease %q rejected", tag)
		}
	}
}

func TestInventoryForDirUsesRequestedBuildTarget(t *testing.T) {
	directory := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/api\n\ngo 1.22\n")
	write("api.go", "package api\ntype Common struct{}\n")
	write("api_linux.go", "//go:build linux\n\npackage api\ntype LinuxOnly struct{}\n")
	write("api_windows.go", "//go:build windows\n\npackage api\n\nimport \"syscall\"\n\nvar _ = syscall.UTF16FromString\ntype WindowsOnly struct{}\n")
	inventory, err := inventoryForDir(context.Background(), directory, "go", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inventory, "type WindowsOnly struct {  }") || strings.Contains(inventory, "LinuxOnly") {
		t.Fatalf("windows inventory selected wrong files:\n%s", inventory)
	}
}

func TestConsumerCompilerFixtures(t *testing.T) {
	baseline := `package api

type Contract interface { Keep() string }
type Key struct { ID int }
type Options struct { Name string }
var Limit int
func Keep(value string) string { return value }
`
	tests := []struct {
		name   string
		api    string
		passes bool
	}{
		{name: "baseline", api: baseline, passes: true},
		{name: "removed function", api: strings.Replace(baseline, "func Keep(value string) string { return value }\n", "", 1), passes: false},
		{name: "signature change", api: strings.Replace(baseline, "func Keep(value string) string", "func Keep(value int) string", 1), passes: false},
		{name: "interface method addition", api: strings.Replace(baseline, "type Contract interface { Keep() string }", "type Contract interface { Keep() string; Extra() }", 1), passes: false},
		{name: "variable type change", api: strings.Replace(baseline, "var Limit int", "var Limit string", 1), passes: false},
		{name: "additive function and keyed field", api: strings.Replace(strings.Replace(baseline, "type Options struct { Name string }", "type Options struct { Name string; Enabled bool }", 1), "func Keep(value string) string { return value }", "func Keep(value string) string { return value }\nfunc Extra() error { return nil }", 1), passes: true},
		{name: "non-comparable additive field", api: strings.Replace(baseline, "type Key struct { ID int }", "type Key struct { ID int; Values []string }", 1), passes: false},
		{name: "non-comparable private field", api: strings.Replace(baseline, "type Key struct { ID int }", "type Key struct { ID int; values []string }", 1), passes: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := compileConsumerFixture(t, test.api); got != test.passes {
				t.Fatalf("consumer compile = %t, want %t", got, test.passes)
			}
		})
	}
}

func compileConsumerFixture(t *testing.T, api string) bool {
	t.Helper()
	directory := t.TempDir()
	apiDirectory := filepath.Join(directory, "api")
	consumerDirectory := filepath.Join(directory, "consumer")
	for path, content := range map[string]string{
		filepath.Join(apiDirectory, "go.mod"):      "module example.com/api\n\ngo 1.22\n",
		filepath.Join(apiDirectory, "api.go"):      api,
		filepath.Join(consumerDirectory, "go.mod"): "module example.com/consumer\n\ngo 1.22\n\nrequire example.com/api v0.0.0\nreplace example.com/api => ../api\n",
		filepath.Join(consumerDirectory, "consumer_test.go"): `package consumer

import (
	"testing"
	"example.com/api"
)

type implementation struct{}
func (implementation) Keep() string { return "ok" }

func TestConsumerSurface(t *testing.T) {
	var _ api.Contract = implementation{}
	var _ int = api.Limit
	if api.Keep("ok") != "ok" { t.Fatal("unexpected result") }
	_ = api.Options{Name: "keyed"}
	_ = map[api.Key]struct{}{api.Key{ID: 1}: {}}
}
`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = consumerDirectory
	return command.Run() == nil
}

func TestLatestStableV1TagExcludesCandidateAndFutureTags(t *testing.T) {
	for _, prior := range []bool{true, false} {
		t.Run(fmt.Sprint(prior), func(t *testing.T) {
			directory := t.TempDir()
			command := func(args ...string) string {
				t.Helper()
				c := exec.Command("git", append([]string{"-C", directory}, args...)...)
				output, err := c.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, output)
				}
				return strings.TrimSpace(string(output))
			}
			command("init", "--quiet")
			t.Setenv("GIT_DIR", filepath.Join(directory, ".git"))
			t.Setenv("GIT_WORK_TREE", directory)
			command("config", "user.name", "Ben Ranford")
			command("config", "user.email", "84072202+ben-ranford@users.noreply.github.com")
			command("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "baseline")
			if prior {
				command("tag", "-a", "v1.0.0", "-m", "prior stable")
			}
			command("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "candidate")
			candidate := command("rev-parse", "HEAD")
			command("tag", "-a", "v1.1.0", "-m", "candidate stable")
			check := func() {
				t.Helper()
				got, err := latestStableV1Tag(context.Background())
				if prior {
					if err != nil || got != "v1.0.0" {
						t.Fatalf("baseline=%q err=%v", got, err)
					}
				} else if err == nil {
					t.Fatalf("candidate-only stable baseline accepted: %s", got)
				}
			}
			check()
			command("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "future")
			command("tag", "-a", "v1.2.0", "-m", "future stable")
			command("checkout", "--detach", candidate)
			check()
		})
	}
}

func TestArchiveEntryTargetRejectsNonLocalPaths(t *testing.T) {
	root := t.TempDir()
	invalid := []string{"", ".", "..", "../escape", "nested/../../escape", filepath.Join(string(filepath.Separator), "escape")}
	if runtime.GOOS == "windows" {
		invalid = append(invalid, `..\escape`, `C:\escape`, "CON")
	}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			if target, err := archiveEntryTarget(root, name); err == nil {
				t.Fatalf("unsafe archive path %q accepted as %q", name, target)
			}
		})
	}
}

func TestArchiveEntryTargetPreservesNestedLocalPaths(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"go.mod", "nested/file.go", "nested/../file.go"} {
		target, err := archiveEntryTarget(root, name)
		if err != nil || target != filepath.Join(root, name) {
			t.Fatalf("archive path %q: target=%q err=%v", name, target, err)
		}
	}
}
