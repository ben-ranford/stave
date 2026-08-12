package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	staveconfig "github.com/ben-ranford/stave/config"
	configschema "github.com/ben-ranford/stave/schema/config"
	"github.com/ben-ranford/stave/semantic"
)

type goListModule struct {
	Path    string
	Version string
	Main    bool
}

type goListPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Imports    []string
	Standard   bool
	Module     *goListModule
}

type packageInfo struct {
	ImportPath string   `json:"importPath"`
	Dir        string   `json:"dir"`
	Imports    []string `json:"imports,omitempty"`
}

type dependencyInventory struct {
	ModulePath      string        `json:"modulePath"`
	GoVersion       string        `json:"goVersion"`
	Packages        []packageInfo `json:"packages"`
	ExternalModules []moduleInfo  `json:"externalModules"`
}

type moduleInfo struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

type licenseInventory struct {
	ModulePath        string       `json:"modulePath"`
	RepositoryLicense licenseFile  `json:"repositoryLicense"`
	ExternalModules   []moduleInfo `json:"externalModules"`
}

type licenseFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type traceabilityManifest struct {
	ModulePath           string          `json:"modulePath"`
	GoVersion            string          `json:"goVersion"`
	ConfigSchemaVersion  string          `json:"configSchemaVersion"`
	ConfigSchemaRequired []string        `json:"configSchemaRequired"`
	SemanticTreeSchema   string          `json:"semanticTreeSchemaVersion"`
	NodeIDAlgorithm      string          `json:"nodeIdAlgorithm"`
	SchemaArtifacts      []fileDigest    `json:"schemaArtifacts"`
	CommandRenderTrace   []commandRender `json:"commandRenderTrace"`
	AtlasProofMatrix     commandRender   `json:"atlasProofMatrix"`
}

type commandRender struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type fileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("expected subcommand")
	}

	switch args[0] {
	case "public-api":
		fs := flag.NewFlagSet("public-api", flag.ContinueOnError)
		writePath := fs.String("write", "", "write output to path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		pkgs, err := listPackages(ctx, false)
		if err != nil {
			return err
		}
		modulePath, err := modulePathFor(pkgs)
		if err != nil {
			return err
		}
		content, err := renderPublicAPI(modulePath, pkgs)
		if err != nil {
			return err
		}
		return writeOrStdout(*writePath, []byte(content))
	case "dependency-inventory":
		fs := flag.NewFlagSet("dependency-inventory", flag.ContinueOnError)
		writePath := fs.String("write", "", "write output to path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		content, err := renderDependencyInventory(ctx)
		if err != nil {
			return err
		}
		return writeJSON(*writePath, content)
	case "license-inventory":
		fs := flag.NewFlagSet("license-inventory", flag.ContinueOnError)
		writePath := fs.String("write", "", "write output to path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		content, err := renderLicenseInventory(ctx)
		if err != nil {
			return err
		}
		return writeJSON(*writePath, content)
	case "traceability":
		fs := flag.NewFlagSet("traceability", flag.ContinueOnError)
		writePath := fs.String("write", "", "write output to path")
		atlasOutput := fs.String("atlas-output", "", "write atlas render output")
		atlasMatrixOutput := fs.String("atlas-matrix-output", "", "write atlas proof matrix")
		lopperOutput := fs.String("lopper-output", "", "write lopper render output")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		content, renders, err := renderTraceability(ctx)
		if err != nil {
			return err
		}
		if err := writeJSON(*writePath, content); err != nil {
			return err
		}
		if *atlasOutput != "" {
			if err := writeFile(*atlasOutput, renders["atlas"]); err != nil {
				return err
			}
		}
		if *atlasMatrixOutput != "" {
			if err := writeFile(*atlasMatrixOutput, renders["atlas-matrix"]); err != nil {
				return err
			}
		}
		if *lopperOutput != "" {
			if err := writeFile(*lopperOutput, renders["lopper"]); err != nil {
				return err
			}
		}
		return nil
	case "boundary-check":
		return boundaryCheck(ctx)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func renderPublicAPI(modulePath string, pkgs []goListPackage) (string, error) {
	exported := make(map[string][]string)
	fset := token.NewFileSet()

	for _, pkg := range pkgs {
		if pkg.ImportPath == "" || !strings.HasPrefix(pkg.ImportPath, modulePath) {
			continue
		}
		if strings.Contains(pkg.ImportPath, "/internal/") || strings.Contains(pkg.ImportPath, "/cmd/") || strings.Contains(pkg.ImportPath, "/scripts/") {
			continue
		}

		filePaths := make([]string, 0, len(pkg.GoFiles))
		for _, file := range pkg.GoFiles {
			filePaths = append(filePaths, filepath.Join(pkg.Dir, file))
		}
		parsed, err := parser.ParseDir(fset, pkg.Dir, func(info os.FileInfo) bool {
			name := info.Name()
			if strings.HasSuffix(name, "_test.go") {
				return false
			}
			for _, path := range filePaths {
				if filepath.Base(path) == name {
					return true
				}
			}
			return false
		}, parser.SkipObjectResolution)
		if err != nil {
			return "", err
		}

		entries := []string{}
		for _, parsedPkg := range parsed {
			for _, file := range parsedPkg.Files {
				for _, decl := range file.Decls {
					switch d := decl.(type) {
					case *ast.GenDecl:
						switch d.Tok {
						case token.CONST, token.VAR:
							label := "var"
							if d.Tok == token.CONST {
								label = "const"
							}
							for _, spec := range d.Specs {
								valueSpec, ok := spec.(*ast.ValueSpec)
								if !ok {
									continue
								}
								for _, name := range valueSpec.Names {
									if ast.IsExported(name.Name) {
										entries = append(entries, fmt.Sprintf("%s %s", label, name.Name))
									}
								}
							}
						case token.TYPE:
							for _, spec := range d.Specs {
								typeSpec, ok := spec.(*ast.TypeSpec)
								if ok && ast.IsExported(typeSpec.Name.Name) {
									entries = append(entries, typeDeclaration(fset, typeSpec))
								}
							}
						}
					case *ast.FuncDecl:
						if d.Name == nil || !ast.IsExported(d.Name.Name) {
							continue
						}
						signature := funcSignature(fset, d.Type)
						if d.Recv == nil || len(d.Recv.List) == 0 {
							entries = append(entries, fmt.Sprintf("func %s%s", d.Name.Name, signature))
							continue
						}
						receiver := exprString(fset, d.Recv.List[0].Type)
						entries = append(entries, fmt.Sprintf("method (%s) %s%s", receiver, d.Name.Name, signature))
					}
				}
			}
		}

		sort.Strings(entries)
		exported[pkg.ImportPath] = entries
	}

	paths := make([]string, 0, len(exported))
	for path := range exported {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var out strings.Builder
	out.WriteString("# Public API inventory\n")
	out.WriteString(fmt.Sprintf("module %s\n\n", modulePath))
	for _, path := range paths {
		out.WriteString(fmt.Sprintf("[%s]\n", path))
		for _, entry := range exported[path] {
			out.WriteString(entry)
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n") + "\n", nil
}

func renderDependencyInventory(ctx context.Context) (dependencyInventory, error) {
	pkgs, err := listPackages(ctx, false)
	if err != nil {
		return dependencyInventory{}, err
	}
	deps, err := listPackages(ctx, true)
	if err != nil {
		return dependencyInventory{}, err
	}
	modulePath, err := modulePathFor(pkgs)
	if err != nil {
		return dependencyInventory{}, err
	}

	packages := make([]packageInfo, 0, len(pkgs))
	repoRoot := mustRepoRoot()
	for _, pkg := range pkgs {
		relDir, relErr := filepath.Rel(repoRoot, pkg.Dir)
		if relErr != nil {
			relDir = pkg.Dir
		}
		relDir = filepath.ToSlash(relDir)
		if relDir == "." {
			relDir = "."
		}
		packages = append(packages, packageInfo{
			ImportPath: pkg.ImportPath,
			Dir:        relDir,
			Imports:    sortedCopy(pkg.Imports),
		})
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })

	seenModules := map[string]moduleInfo{}
	for _, dep := range deps {
		if dep.Standard || dep.Module == nil || dep.Module.Main || dep.Module.Path == modulePath {
			continue
		}
		seenModules[dep.Module.Path] = moduleInfo{Path: dep.Module.Path, Version: dep.Module.Version}
	}

	modules := make([]moduleInfo, 0, len(seenModules))
	for _, module := range seenModules {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })

	return dependencyInventory{
		ModulePath:      modulePath,
		GoVersion:       readGoVersion(),
		Packages:        packages,
		ExternalModules: modules,
	}, nil
}

func renderLicenseInventory(ctx context.Context) (licenseInventory, error) {
	dependencyContent, err := renderDependencyInventory(ctx)
	if err != nil {
		return licenseInventory{}, err
	}
	licenseBytes, err := os.ReadFile(filepath.Join(mustRepoRoot(), "LICENSE"))
	if err != nil {
		return licenseInventory{}, err
	}
	sum := sha256.Sum256(licenseBytes)

	return licenseInventory{
		ModulePath: dependencyContent.ModulePath,
		RepositoryLicense: licenseFile{
			Path:   "LICENSE",
			SHA256: hex.EncodeToString(sum[:]),
		},
		ExternalModules: dependencyContent.ExternalModules,
	}, nil
}

func renderTraceability(ctx context.Context) (traceabilityManifest, map[string][]byte, error) {
	pkgs, err := listPackages(ctx, false)
	if err != nil {
		return traceabilityManifest{}, nil, err
	}
	modulePath, err := modulePathFor(pkgs)
	if err != nil {
		return traceabilityManifest{}, nil, err
	}

	semanticSchema, err := readSemanticSchemaVersion()
	if err != nil {
		return traceabilityManifest{}, nil, err
	}

	renders := map[string][]byte{}
	trace := make([]commandRender, 0, 2)
	schemaArtifacts := make([]fileDigest, 0, 4)
	for _, relativePath := range []string{
		"schema/action/definition.json",
		"schema/config/config.json",
		"schema/protocol/protocol.json",
		"schema/semantic/snapshot.json",
	} {
		content, err := os.ReadFile(filepath.Join(mustRepoRoot(), relativePath))
		if err != nil {
			return traceabilityManifest{}, nil, err
		}
		sum := sha256.Sum256(content)
		schemaArtifacts = append(schemaArtifacts, fileDigest{
			Path:   relativePath,
			SHA256: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(schemaArtifacts, func(i, j int) bool { return schemaArtifacts[i].Path < schemaArtifacts[j].Path })

	for _, command := range []struct {
		name string
		path string
	}{
		{name: "atlas", path: "./cmd/atlas"},
		{name: "lopper", path: "./cmd/lopper"},
	} {
		output, err := execCommand(ctx, "go", "run", command.path)
		if err != nil {
			return traceabilityManifest{}, nil, err
		}
		renders[command.name] = output
		sum := sha256.Sum256(output)
		trace = append(trace, commandRender{
			Name:   command.name,
			Path:   command.path,
			SHA256: hex.EncodeToString(sum[:]),
			Bytes:  len(output),
		})
	}
	sort.Slice(trace, func(i, j int) bool { return trace[i].Name < trace[j].Name })
	atlasMatrix, err := execCommand(ctx, "go", "run", "./cmd/atlas", "-matrix")
	if err != nil {
		return traceabilityManifest{}, nil, err
	}
	renders["atlas-matrix"] = atlasMatrix
	atlasMatrixSum := sha256.Sum256(atlasMatrix)
	atlasMatrixTrace := commandRender{
		Name: "atlas-proof-matrix", Path: "./cmd/atlas -matrix",
		SHA256: hex.EncodeToString(atlasMatrixSum[:]), Bytes: len(atlasMatrix),
	}

	return traceabilityManifest{
		ModulePath:           modulePath,
		GoVersion:            readGoVersion(),
		ConfigSchemaVersion:  staveconfig.Defaults().SchemaVersion,
		ConfigSchemaRequired: sortedCopy(configschema.Required),
		SemanticTreeSchema:   semanticSchema,
		NodeIDAlgorithm:      semantic.NodeIDNormalizationPolicy,
		SchemaArtifacts:      schemaArtifacts,
		CommandRenderTrace:   trace,
		AtlasProofMatrix:     atlasMatrixTrace,
	}, renders, nil
}

func boundaryCheck(ctx context.Context) error {
	pkgs, err := listPackages(ctx, false)
	if err != nil {
		return err
	}
	modulePath, err := modulePathFor(pkgs)
	if err != nil {
		return err
	}

	knownPackages := map[string]bool{}
	for _, pkg := range pkgs {
		if pkg.ImportPath != "" {
			knownPackages[pkg.ImportPath] = true
		}
	}

	violations := []string{}
	for _, pkg := range pkgs {
		if pkg.ImportPath == "" || !strings.HasPrefix(pkg.ImportPath, modulePath) {
			continue
		}
		for _, imp := range pkg.Imports {
			if isStdlibImport(imp) {
				continue
			}
			if !strings.HasPrefix(imp, modulePath) {
				violations = append(violations, fmt.Sprintf("%s imports external package %s", pkg.ImportPath, imp))
				continue
			}
			if !knownPackages[imp] {
				violations = append(violations, fmt.Sprintf("%s imports missing in-module package %s", pkg.ImportPath, imp))
			}
			if strings.HasPrefix(imp, modulePath+"/cmd/") {
				violations = append(violations, fmt.Sprintf("%s imports command package %s", pkg.ImportPath, imp))
			}
			if strings.HasPrefix(imp, modulePath+"/scripts/") && !strings.HasPrefix(pkg.ImportPath, modulePath+"/scripts/") {
				violations = append(violations, fmt.Sprintf("%s imports tooling package %s", pkg.ImportPath, imp))
			}
			if strings.HasPrefix(pkg.ImportPath, modulePath+"/schema/") && strings.HasPrefix(imp, modulePath+"/") && !strings.HasPrefix(imp, modulePath+"/schema/") {
				violations = append(violations, fmt.Sprintf("%s must stay schema-only but imports %s", pkg.ImportPath, imp))
			}
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "\n"))
	}

	fmt.Printf("import boundary checks passed for %d packages\n", len(pkgs))
	return nil
}

func listPackages(ctx context.Context, withDeps bool) ([]goListPackage, error) {
	args := []string{"list", "-e", "-json"}
	if withDeps {
		args = append(args, "-deps")
	}
	args = append(args, "./...")

	output, err := execCommand(ctx, "go", args...)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(output))
	packages := []goListPackage{}
	for {
		var pkg goListPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

func modulePathFor(pkgs []goListPackage) (string, error) {
	for _, pkg := range pkgs {
		if pkg.Module != nil && pkg.Module.Main {
			return pkg.Module.Path, nil
		}
	}
	return "", errors.New("failed to resolve main module path")
}

func readGoVersion() string {
	content, err := os.ReadFile(filepath.Join(mustRepoRoot(), "go.mod"))
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?m)^go\s+([0-9.]+)\s*$`)
	match := re.FindStringSubmatch(string(content))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func readSemanticSchemaVersion() (string, error) {
	content, err := os.ReadFile(filepath.Join(mustRepoRoot(), "semantic", "semantic.go"))
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`schemaVersion:\s*"([^"]+)"`)
	match := re.FindStringSubmatch(string(content))
	if len(match) != 2 {
		return "", errors.New("semantic schema version not found")
	}
	return match[1], nil
}

func writeJSON(path string, v any) error {
	content, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return writeOrStdout(path, content)
}

func writeOrStdout(path string, content []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(content)
		return err
	}
	return writeFile(path, content)
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func execCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = mustRepoRoot()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, stderr.String())
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func exprString(fset *token.FileSet, expr any) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, expr)
	return buf.String()
}

func funcSignature(fset *token.FileSet, fn *ast.FuncType) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, fn)
	return strings.TrimPrefix(buf.String(), "func")
}

func typeDeclaration(fset *token.FileSet, spec *ast.TypeSpec) string {
	var out strings.Builder
	out.WriteString("type ")
	out.WriteString(spec.Name.Name)
	if spec.TypeParams != nil {
		out.WriteString(typeParameterList(fset, spec.TypeParams))
	}
	if spec.Assign.IsValid() {
		out.WriteString(" =")
	}
	out.WriteByte(' ')
	switch typ := spec.Type.(type) {
	case *ast.StructType:
		out.WriteString("struct { ")
		out.WriteString(strings.Join(publicStructFields(fset, typ.Fields), "; "))
		out.WriteString(" }")
	case *ast.InterfaceType:
		out.WriteString("interface { ")
		out.WriteString(strings.Join(publicInterfaceElements(fset, typ.Methods), "; "))
		out.WriteString(" }")
	default:
		out.WriteString(exprString(fset, spec.Type))
	}
	return out.String()
}

func publicStructFields(fset *token.FileSet, fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	out := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		names := exportedFieldNames(field.Names)
		if len(field.Names) > 0 && len(names) == 0 {
			continue
		}
		declaration := exprString(fset, field.Type)
		if len(names) > 0 {
			declaration = strings.Join(names, ", ") + " " + declaration
		}
		if field.Tag != nil {
			declaration += " " + field.Tag.Value
		}
		out = append(out, declaration)
	}
	return out
}

func publicInterfaceElements(fset *token.FileSet, fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	out := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		names := exportedFieldNames(field.Names)
		if len(field.Names) > 0 && len(names) == 0 {
			continue
		}
		declaration := exprString(fset, field.Type)
		if fn, ok := field.Type.(*ast.FuncType); ok {
			declaration = funcSignature(fset, fn)
		}
		if len(names) > 0 {
			declaration = strings.Join(names, ", ") + declaration
		}
		out = append(out, declaration)
	}
	return out
}

func typeParameterList(fset *token.FileSet, fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	parameters := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		names := make([]string, 0, len(field.Names))
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		parameters = append(parameters, strings.Join(names, ", ")+" "+exprString(fset, field.Type))
	}
	return "[" + strings.Join(parameters, ", ") + "]"
}

func exportedFieldNames(names []*ast.Ident) []string {
	exported := make([]string, 0, len(names))
	for _, name := range names {
		if ast.IsExported(name.Name) {
			exported = append(exported, name.Name)
		}
	}
	return exported
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func isStdlibImport(path string) bool {
	segment := path
	if idx := strings.Index(segment, "/"); idx >= 0 {
		segment = segment[:idx]
	}
	return !strings.Contains(segment, ".")
}

func mustRepoRoot() string {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return root
}
