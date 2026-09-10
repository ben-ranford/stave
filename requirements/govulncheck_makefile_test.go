package requirements

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGovulncheckStopsAtFirstNestedModuleFailureWithoutShellErrexit(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), makefile, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, module := range []string{"bubbletea", "lipgloss", "ssh"} {
		dir := filepath.Join(root, "adapters", module)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example/"+module+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	guardDir := filepath.Join(root, "scripts", "rigor", "workflow-guard")
	if err := os.MkdirAll(guardDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(guardDir, "go.mod"), []byte("module example/workflow-guard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(root, "scripts", "rigor", "install-tools.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(root, ".cache", "rigor", "bin", "govulncheck"), `#!/bin/sh
printf '%s\n' "$PWD" >> "$SCAN_LOG"
case "${FAIL_AT:-}" in
  root) if [ "$PWD" = "$TEST_ROOT" ]; then exit 23; fi ;;
  bubbletea|lipgloss|ssh) if [ "$(basename "$PWD")" = "$FAIL_AT" ]; then exit 23; fi ;;
esac
`)

	cases := []struct {
		name, failAt string
		want         []string
		success      bool
	}{
		{name: "root failure", failAt: "root", want: []string{""}},
		{name: "bubbletea failure", failAt: "bubbletea", want: []string{"", "adapters/bubbletea"}},
		{name: "lipgloss failure", failAt: "lipgloss", want: []string{"", "adapters/bubbletea", "adapters/lipgloss"}},
		{name: "all modules pass", want: []string{"", "adapters/bubbletea", "adapters/lipgloss", "adapters/ssh", "scripts/rigor/workflow-guard"}, success: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "scan.log")
			command := exec.Command("make", ".SHELLFLAGS=-c", "govulncheck")
			command.Dir = root
			command.Env = append(os.Environ(), "FAIL_AT="+tc.failAt, "SCAN_LOG="+log, "TEST_ROOT="+realRoot)
			output, err := command.CombinedOutput()
			if tc.success && err != nil {
				t.Fatalf("govulncheck failed: %v\n%s", err, output)
			}
			if !tc.success && err == nil {
				t.Fatalf("govulncheck succeeded after %s\n%s", tc.failAt, output)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, dir := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				realDir, err := filepath.EvalSymlinks(dir)
				if err != nil {
					t.Fatal(err)
				}
				rel, err := filepath.Rel(realRoot, realDir)
				if err != nil {
					t.Fatal(err)
				}
				if rel == "." {
					rel = ""
				}
				got = append(got, filepath.ToSlash(rel))
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("scanned %v, want %v", got, tc.want)
			}
		})
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}
