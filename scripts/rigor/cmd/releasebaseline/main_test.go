package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func TestConsumerCompilerFixtures(t *testing.T) {
	baseline := `package api

type Contract interface { Keep() string }
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
