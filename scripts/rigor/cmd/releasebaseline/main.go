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
	"go/token"
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

var (
	stableV1Tag     = regexp.MustCompile(`^v1\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	prereleaseV1Tag = regexp.MustCompile(`^v1\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-((0|[1-9][0-9]*)|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(\.((0|[1-9][0-9]*)|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
)

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
	goBinary := flags.String("go", "go", "Go executable used for both inventories")
	goos := flags.String("goos", "", "target GOOS for build-tag selection")
	goarch := flags.String("goarch", "", "target GOARCH for build-tag selection")
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
	if (*goos == "") != (*goarch == "") {
		return errors.New("release-baseline requires both --goos and --goarch")
	}

	tag := *baselineTag
	if !*development {
		var err error
		tag, err = latestStableV1Tag(ctx)
		if err != nil {
			return err
		}
	}
	if *development && !prereleaseV1Tag.MatchString(tag) {
		return fmt.Errorf("baseline tag %q is not a v1 prerelease semver tag", tag)
	}
	base, err := loadBaseline(ctx, tag, *goBinary, *goos, *goarch)
	if err != nil {
		return err
	}
	candidate, err := currentInventory(ctx, *goBinary, *goos, *goarch)
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
	candidateFloor, err := readGoFloor(mustRepoRoot())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "mode=%s\nbaseline_tag=%s\nbaseline_commit=%s\nbaseline_go_floor=%s\ncandidate_go_floor=%s\nstatus=compatible\n", mode, base.Tag, base.Commit, base.GoFloor, candidateFloor)
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
		objectType, err := git(ctx, "cat-file", "-t", tag)
		if err != nil || strings.TrimSpace(string(objectType)) != "tag" {
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

func loadBaseline(ctx context.Context, tag, goBinary, goos, goarch string) (baseline, error) {
	if !stableV1Tag.MatchString(tag) && !prereleaseV1Tag.MatchString(tag) {
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
	inventory, err := inventoryForDir(ctx, directory, goBinary, goos, goarch)
	if err != nil {
		return baseline{}, fmt.Errorf("inventory baseline tag %q: %w", tag, err)
	}
	floor, err := readGoFloor(directory)
	if err != nil {
		return baseline{}, err
	}
	return baseline{Tag: tag, Commit: strings.TrimSpace(string(commit)), GoFloor: floor, Inventory: inventory}, nil
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

func currentInventory(ctx context.Context, goBinary, goos, goarch string) (string, error) {
	return inventoryForDir(ctx, mustRepoRoot(), goBinary, goos, goarch)
}

func inventoryForDir(ctx context.Context, directory, goBinary, goos, goarch string) (string, error) {
	args := []string{"run", "./scripts/rigor/cmd/rigor", "public-api", "--dir", directory}
	if goos != "" {
		args = append(args, "--goos", goos, "--goarch", goarch)
	}
	command := exec.CommandContext(ctx, goBinary, args...)
	command.Dir = mustRepoRoot()
	command.Env = append(os.Environ(), "STAVE_RIGOR_GO="+goBinary)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("generate public API inventory: %w\n%s", err, output)
	}
	return string(output), nil
}

func readGoFloor(directory string) (string, error) {
	data, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read Go floor: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			return fields[1], nil
		}
	}
	return "", errors.New("go floor is missing from go.mod")
}

func compareInventories(baselineInventory, candidateInventory string) error {
	baseline := parseInventory(baselineInventory)
	candidate := parseInventory(candidateInventory)
	var failures []string
	for pkg, declarations := range baseline {
		candidateDeclarations, exists := candidate[pkg]
		if !exists {
			failures = append(failures, pkg+": removed package")
			continue
		}
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
	candidateIndex := 0
	for _, field := range baselineFields {
		for candidateIndex < len(candidateFields) && candidateFields[candidateIndex] != field {
			candidateIndex++
		}
		if candidateIndex == len(candidateFields) {
			return false
		}
		candidateIndex++
	}
	return true
}

func structFields(declaration string) []string {
	body := strings.TrimSuffix(strings.SplitN(declaration, structMarker, 2)[1], " }")
	fields := []string{}
	if body == "" {
		return fields
	}
	start, curly, square, paren := 0, 0, 0, 0
	var quote rune
	escaped := false
	for index, character := range body {
		if quote != 0 {
			if quote != '`' && character == '\\' && !escaped {
				escaped = true
				continue
			}
			if character == quote && !escaped {
				quote = 0
			}
			escaped = false
			continue
		}
		switch character {
		case '\'', '"', '`':
			quote = character
		case '{':
			curly++
		case '}':
			curly--
		case '[':
			square++
		case ']':
			square--
		case '(':
			paren++
		case ')':
			paren--
		case ';':
			if curly == 0 && square == 0 && paren == 0 {
				fields = append(fields, normalizedStructFieldDeclarations(strings.TrimSpace(body[start:index]))...)
				start = index + 1
			}
		}
	}
	fields = append(fields, normalizedStructFieldDeclarations(strings.TrimSpace(body[start:]))...)
	return fields
}

// normalizedStructFieldDeclarations expands only grouped named fields. This
// keeps the inventory's existing type rendering while making `A, B T` and
// `A T; B T` compare as the same ordered fields.
func normalizedStructFieldDeclarations(declaration string) []string {
	for index, character := range declaration {
		if character != ' ' && character != '\t' {
			continue
		}
		names := strings.TrimSpace(declaration[:index])
		if names == "" || !groupedFieldNames(names) {
			continue
		}
		typeDeclaration := declaration[index:]
		fields := strings.Split(names, ",")
		out := make([]string, 0, len(fields))
		for _, name := range fields {
			out = append(out, strings.TrimSpace(name)+typeDeclaration)
		}
		return out
	}
	return []string{declaration}
}

func groupedFieldNames(names string) bool {
	fields := strings.Split(names, ",")
	if len(fields) < 2 {
		return false
	}
	for _, field := range fields {
		if !token.IsIdentifier(strings.TrimSpace(field)) {
			return false
		}
	}
	return true
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
