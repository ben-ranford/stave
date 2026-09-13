// Command releasebaseline checks a candidate public API against an immutable
// v1 source tag without trusting the candidate's checked-in inventory.
package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	gitRevParse  = "rev-parse"
	methodPrefix = "method "
	structMarker = " struct {"
)

var stableV1Tag = regexp.MustCompile(`^v1\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type baseline struct {
	Tag       string
	Commit    string
	GoFloor   string
	Inventory string
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("release-baseline", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	development := flags.Bool("development", false, "allow an explicitly named prerelease development baseline")
	baselineTag := flags.String("baseline-tag", "", "baseline tag; required for development mode")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("release-baseline accepts no positional arguments")
	}
	if *development && *baselineTag == "" {
		return errors.New("development comparison requires --baseline-tag")
	}
	if !*development && *baselineTag != "" {
		return errors.New("stable comparison selects its own stable v1 tag; use --development for an explicit prerelease baseline")
	}

	tag := *baselineTag
	if !*development {
		var err error
		tag, err = latestStableV1Tag(ctx)
		if err != nil {
			return err
		}
	}
	base, err := loadBaseline(ctx, tag)
	if err != nil {
		return err
	}
	candidate, err := currentInventory(ctx)
	if err != nil {
		return err
	}
	if err := compareInventories(base.Inventory, candidate); err != nil {
		return fmt.Errorf("minor-release API compatibility failed against %s (%s): %w", base.Tag, base.Commit, err)
	}
	mode := "stable"
	if *development {
		mode = "development-prerelease"
	}
	_, err = fmt.Fprintf(stdout, "mode=%s\nbaseline_tag=%s\nbaseline_commit=%s\nbaseline_go_floor=%s\ncandidate_go_floor=%s\nstatus=compatible\n", mode, base.Tag, base.Commit, base.GoFloor, readGoFloor(mustRepoRoot()))
	return err
}

func latestStableV1Tag(ctx context.Context) (string, error) {
	head, err := git(ctx, gitRevParse, "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	output, err := git(ctx, "tag", "--list", "v1.*", "--merged", "HEAD", "--sort=-v:refname")
	if err != nil {
		return "", err
	}
	for _, tag := range strings.Fields(string(output)) {
		if !stableV1Tag.MatchString(tag) {
			continue
		}
		commit, err := git(ctx, gitRevParse, tag+"^{commit}")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(string(commit)) != strings.TrimSpace(string(head)) {
			return tag, nil
		}
	}
	available := strings.Join(strings.Fields(string(output)), ", ")
	if available == "" {
		available = "none"
	}
	return "", fmt.Errorf("no earlier stable v1 tag is available for the minor-release compatibility gate; ancestor v1 tags: %s; create the stable baseline through issue #2 before GA", available)
}

func loadBaseline(ctx context.Context, tag string) (baseline, error) {
	if !strings.HasPrefix(tag, "v1.") || (!stableV1Tag.MatchString(tag) && !strings.Contains(tag, "-")) {
		return baseline{}, fmt.Errorf("baseline tag %q is not a v1 semver tag", tag)
	}
	objectType, err := git(ctx, "cat-file", "-t", tag)
	if err != nil {
		return baseline{}, fmt.Errorf("read baseline tag %q: %w", tag, err)
	}
	if strings.TrimSpace(string(objectType)) != "tag" {
		return baseline{}, fmt.Errorf("baseline tag %q must be an annotated immutable tag", tag)
	}
	commit, err := git(ctx, gitRevParse, tag+"^{commit}")
	if err != nil {
		return baseline{}, err
	}
	directory, err := archiveTag(ctx, tag)
	if err != nil {
		return baseline{}, err
	}
	defer os.RemoveAll(directory)
	inventory, err := inventoryForDir(ctx, directory)
	if err != nil {
		return baseline{}, fmt.Errorf("inventory baseline tag %q: %w", tag, err)
	}
	return baseline{Tag: tag, Commit: strings.TrimSpace(string(commit)), GoFloor: readGoFloor(directory), Inventory: inventory}, nil
}

func archiveTag(ctx context.Context, tag string) (string, error) {
	directory, err := os.MkdirTemp("", "stave-release-baseline-")
	if err != nil {
		return "", err
	}
	archive, err := git(ctx, "archive", "--format=tar", tag)
	if err != nil {
		_ = os.RemoveAll(directory)
		return "", err
	}
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = os.RemoveAll(directory)
			return "", err
		}
		target, err := archiveEntryTarget(directory, header.Name)
		if err != nil {
			_ = os.RemoveAll(directory)
			return "", err
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				_ = os.RemoveAll(directory)
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				_ = os.RemoveAll(directory)
				return "", err
			}
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				_ = os.RemoveAll(directory)
				return "", err
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil {
				_ = os.RemoveAll(directory)
				return "", errors.Join(copyErr, closeErr)
			}
		default:
			_ = os.RemoveAll(directory)
			return "", fmt.Errorf("baseline archive contains unsupported entry %q", header.Name)
		}
	}
	return directory, nil
}

func archiveEntryTarget(directory, name string) (string, error) {
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("baseline archive has unsafe path %q", name)
	}
	target := filepath.Join(directory, name)
	if !strings.HasPrefix(target, directory+string(filepath.Separator)) {
		return "", fmt.Errorf("baseline archive has unsafe path %q", name)
	}
	return target, nil
}

func currentInventory(ctx context.Context) (string, error) {
	return inventoryForDir(ctx, mustRepoRoot())
}

func inventoryForDir(ctx context.Context, directory string) (string, error) {
	command := exec.CommandContext(ctx, "go", "run", "./scripts/rigor/cmd/rigor", "public-api", "--dir", directory)
	command.Dir = mustRepoRoot()
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("generate public API inventory: %w\n%s", err, output)
	}
	return string(output), nil
}

func readGoFloor(directory string) string {
	data, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	return "unknown"
}

func compareInventories(baselineInventory, candidateInventory string) error {
	baseline := parseInventory(baselineInventory)
	candidate := parseInventory(candidateInventory)
	var failures []string
	for pkg, declarations := range baseline {
		candidateDeclarations := candidate[pkg]
		for key, declaration := range declarations {
			current, ok := candidateDeclarations[key]
			if !ok {
				failures = append(failures, pkg+": removed "+declaration)
				continue
			}
			if declaration == current || compatibleStructFieldAddition(declaration, current) {
				continue
			}
			failures = append(failures, pkg+": changed "+declaration+" -> "+current)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	sort.Strings(failures)
	return errors.New(strings.Join(failures, "\n"))
}

func parseInventory(inventory string) map[string]map[string]string {
	packages := map[string]map[string]string{}
	current := ""
	for _, line := range strings.Split(inventory, "\n") {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			packages[current] = map[string]string{}
			continue
		}
		if current == "" || line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "module ") {
			continue
		}
		packages[current][declarationKey(line)] = line
	}
	return packages
}

func declarationKey(declaration string) string {
	if strings.HasPrefix(declaration, "type ") {
		return strings.SplitN(strings.TrimPrefix(declaration, "type "), " ", 2)[0]
	}
	if strings.HasPrefix(declaration, "func ") {
		return "func " + strings.SplitN(strings.TrimPrefix(declaration, "func "), "(", 2)[0]
	}
	if strings.HasPrefix(declaration, methodPrefix) {
		rest := strings.TrimPrefix(declaration, methodPrefix)
		receiverEnd := strings.Index(rest, ") ")
		if receiverEnd >= 0 {
			name := strings.SplitN(rest[receiverEnd+2:], "(", 2)[0]
			return methodPrefix + rest[:receiverEnd+1] + " " + name
		}
	}
	fields := strings.Fields(declaration)
	if len(fields) >= 2 {
		return fields[0] + " " + fields[1]
	}
	return declaration
}

func compatibleStructFieldAddition(baselineDeclaration, candidateDeclaration string) bool {
	if !strings.HasPrefix(baselineDeclaration, "type ") || !strings.Contains(baselineDeclaration, structMarker) || !strings.HasPrefix(candidateDeclaration, strings.SplitN(baselineDeclaration, structMarker, 2)[0]+structMarker) {
		return false
	}
	baselineFields := structFields(baselineDeclaration)
	candidateFields := structFields(candidateDeclaration)
	if len(candidateFields) < len(baselineFields) {
		return false
	}
	for field := range baselineFields {
		if !candidateFields[field] {
			return false
		}
	}
	return true
}

func structFields(declaration string) map[string]bool {
	body := strings.TrimSuffix(strings.SplitN(declaration, structMarker, 2)[1], " }")
	fields := map[string]bool{}
	for _, field := range strings.Split(body, "; ") {
		fields[strings.TrimSpace(field)] = true
	}
	return fields
}

func git(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = mustRepoRoot()
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	return output, nil
}

func mustRepoRoot() string {
	directory, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			panic("repository root not found")
		}
		directory = parent
	}
}
