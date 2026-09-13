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
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	staveconfig "github.com/ben-ranford/stave/config"
	configschema "github.com/ben-ranford/stave/schema/config"
	"github.com/ben-ranford/stave/semantic"
)

const scriptsPackageSuffix = "/scripts/"

type goListModule struct {
	Path    string
	Version string
	Main    bool
	Dir     string
}

type goListPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	Imports    []string
	Standard   bool
	Module     *goListModule
}

type sourceImporter struct {
	fset     *token.FileSet
	packages map[string]goListPackage
	checked  map[string]*types.Package
	standard types.Importer
	sizes    types.Sizes
}

func (i *sourceImporter) Import(path string) (*types.Package, error) {
	if pkg, ok := i.checked[path]; ok {
		return pkg, nil
	}

	source, ok := i.packages[path]
	if !ok {
		return i.standard.Import(path)
	}
	if err := rejectCgoSource(source); err != nil {
		return nil, err
	}

	files := make([]*ast.File, 0, len(source.GoFiles))
	for _, name := range source.GoFiles {
		file, err := parser.ParseFile(i.fset, filepath.Join(source.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("package %s has no Go files", path)
	}

	pkg, err := (&types.Config{Importer: i, Sizes: i.sizes}).Check(path, i.fset, files, nil)
	if err != nil {
		return nil, err
	}
	i.checked[path] = pkg
	return pkg, nil
}

type packageInfo struct {
	ImportPath string   `json:"importPath"`
	Dir        string   `json:"dir"`
	Imports    []string `json:"imports,omitempty"`
}

type dependencyInventory struct {
	ModulePath      string          `json:"modulePath"`
	GoVersion       string          `json:"goVersion"`
	Packages        []packageInfo   `json:"packages"`
	ExternalModules []moduleInfo    `json:"externalModules"`
	ToolingModules  []toolingModule `json:"toolingModules"`
}

type moduleInfo struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

type toolingModule struct {
	Path            string       `json:"path"`
	ModulePath      string       `json:"modulePath"`
	ExternalModules []moduleInfo `json:"externalModules"`
}

type licenseInventory struct {
	ModulePath        string          `json:"modulePath"`
	RepositoryLicense licenseFile     `json:"repositoryLicense"`
	ExternalModules   []moduleInfo    `json:"externalModules"`
	ToolingModules    []toolingModule `json:"toolingModules"`
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
		directory := fs.String("dir", "", "module directory to inventory")
		goos := fs.String("goos", "", "target GOOS for build-tag selection")
		goarch := fs.String("goarch", "", "target GOARCH for build-tag selection")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if (*goos == "") != (*goarch == "") {
			return errors.New("public-api requires both --goos and --goarch")
		}
		if *directory == "" {
			*directory = mustRepoRoot()
		}
		pkgs, err := listPackagesInDirForTarget(ctx, *directory, false, *goos, *goarch)
		if err != nil {
			return err
		}
		modulePath, err := modulePathFor(pkgs)
		if err != nil {
			return err
		}
		content, err := renderPublicAPIForTarget(ctx, modulePath, pkgs, *goos, *goarch)
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
	return renderPublicAPIForTarget(context.Background(), modulePath, pkgs, "", "")
}

func renderPublicAPIForTarget(ctx context.Context, modulePath string, pkgs []goListPackage, goos, goarch string) (string, error) {
	exported := make(map[string][]string)
	fset := token.NewFileSet()
	importer := newSourceImporter(ctx, fset, pkgs, goos, goarch)

	for _, pkg := range pkgs {
		if !publicAPIPackage(modulePath, pkg.ImportPath) {
			continue
		}
		if err := rejectCgoSource(pkg); err != nil {
			return "", err
		}
		entries, err := publicAPIEntries(fset, pkg, importer)
		if err != nil {
			return "", err
		}
		exported[pkg.ImportPath] = entries
	}
	return formatPublicAPI(modulePath, exported), nil
}

func rejectCgoSource(pkg goListPackage) error {
	if len(pkg.CgoFiles) == 0 {
		return nil
	}
	return fmt.Errorf("public API inventory does not support cgo source in package %s: %s", pkg.ImportPath, strings.Join(pkg.CgoFiles, ", "))
}

func newSourceImporter(ctx context.Context, fset *token.FileSet, pkgs []goListPackage, goos, goarch string) *sourceImporter {
	packageIndex := make(map[string]goListPackage, len(pkgs))
	for _, pkg := range pkgs {
		packageIndex[pkg.ImportPath] = pkg
	}
	return &sourceImporter{
		fset:     fset,
		packages: packageIndex,
		checked:  make(map[string]*types.Package),
		standard: targetPackageImporter(ctx, fset, packageDirectory(pkgs), rigorGoBinary(), goos, goarch),
		sizes:    targetSizes(goarch),
	}
}

func publicAPIPackage(modulePath, path string) bool {
	return path != "" && strings.HasPrefix(path, modulePath) &&
		!strings.Contains(path, "/internal/") && !strings.Contains(path, "/cmd/") && !strings.Contains(path, scriptsPackageSuffix)
}

func publicAPIEntries(fset *token.FileSet, pkg goListPackage, importer *sourceImporter) ([]string, error) {
	parsed, err := parsePublicAPIPackage(fset, pkg)
	if err != nil {
		return nil, err
	}
	entries := []string{}
	for _, files := range parsed {
		packageName, err := declaredPackageName(files)
		if err != nil {
			return nil, err
		}
		entries = append(entries, "package "+packageName)
		current, err := publicAPIEntriesForFiles(fset, pkg.ImportPath, files, importer)
		if err != nil {
			return nil, err
		}
		entries = append(entries, current...)
	}
	sort.Strings(entries)
	return entries, nil
}

func declaredPackageName(files []*ast.File) (string, error) {
	if len(files) == 0 || files[0].Name == nil || files[0].Name.Name == "" {
		return "", errors.New("public API package has no declared package name")
	}
	name := files[0].Name.Name
	for _, file := range files[1:] {
		if file.Name == nil || file.Name.Name != name {
			return "", errors.New("public API package has inconsistent declared package names")
		}
	}
	return name, nil
}

func parsePublicAPIPackage(fset *token.FileSet, pkg goListPackage) ([][]*ast.File, error) {
	filePaths := make(map[string]bool, len(pkg.GoFiles))
	for _, file := range pkg.GoFiles {
		filePaths[file] = true
	}
	parsed, err := parser.ParseDir(fset, pkg.Dir, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go") && filePaths[info.Name()]
	}, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	packages := make([][]*ast.File, 0, len(parsed))
	for _, parsedPkg := range parsed {
		files := make([]*ast.File, 0, len(parsedPkg.Files))
		for _, file := range parsedPkg.Files {
			files = append(files, file)
		}
		packages = append(packages, files)
	}
	return packages, nil
}

func publicAPIEntriesForFiles(fset *token.FileSet, importPath string, files []*ast.File, importer *sourceImporter) ([]string, error) {
	typeInfo := types.Info{Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object), Types: make(map[ast.Expr]types.TypeAndValue)}
	checkedPkg, err := (&types.Config{Importer: importer, Sizes: importer.sizes}).Check(importPath, fset, files, &typeInfo)
	if err != nil {
		return nil, err
	}
	qualifier := packagePathQualifier(checkedPkg)
	privateInterfaceMethods := privateInterfaceMethodRequirements(&typeInfo, qualifier)
	entries := []string{}
	hiddenTypes := localTypeSpecs(files, &typeInfo)
	seenHiddenTypes := make(map[types.Object]struct{})
	for _, file := range files {
		for _, declaration := range file.Decls {
			current, err := publicAPIEntriesForDeclaration(fset, declaration, &typeInfo, qualifier, privateInterfaceMethods)
			if err != nil {
				return nil, err
			}
			entries = append(entries, current...)
			collector := hiddenTypeCollector{fset: fset, typeInfo: &typeInfo, qualifier: qualifier, hiddenTypes: hiddenTypes, seen: seenHiddenTypes, seenTypes: make(map[types.Type]struct{})}
			collector.collect(declaration)
			entries = append(entries, collector.entries...)
		}
	}
	return entries, nil
}

func privateInterfaceMethodRequirements(typeInfo *types.Info, qualifier types.Qualifier) map[string]struct{} {
	requirements := make(map[string]struct{})
	for _, object := range typeInfo.Defs {
		typeName, ok := object.(*types.TypeName)
		if !ok || !typeName.Exported() || typeName.Pkg() == nil || typeName.Parent() != typeName.Pkg().Scope() {
			continue
		}
		interfaceType, ok := typeName.Type().Underlying().(*types.Interface)
		if !ok {
			continue
		}
		interfaceType.Complete()
		for index := 0; index < interfaceType.NumMethods(); index++ {
			method := interfaceType.Method(index)
			if method.Exported() || method.Pkg() != typeName.Pkg() {
				continue
			}
			requirements[privateMethodRequirement(method, qualifier)] = struct{}{}
		}
	}
	return requirements
}

func privateMethodRequirement(method *types.Func, qualifier types.Qualifier) string {
	signature, _ := method.Type().(*types.Signature)
	return method.Name() + typeSignature(signature, qualifier)
}

func localTypeSpecs(files []*ast.File, typeInfo *types.Info) map[types.Object]*ast.TypeSpec {
	specs := make(map[types.Object]*ast.TypeSpec)
	for _, file := range files {
		for _, declaration := range file.Decls {
			for _, spec := range typeDeclarationSpecs(declaration) {
				object := typeInfo.Defs[spec.Name]
				if object != nil {
					specs[object] = spec
				}
			}
		}
	}
	return specs
}

func typeDeclarationSpecs(declaration ast.Decl) []*ast.TypeSpec {
	general, ok := declaration.(*ast.GenDecl)
	if !ok || general.Tok != token.TYPE {
		return nil
	}
	specs := make([]*ast.TypeSpec, 0, len(general.Specs))
	for _, declaration := range general.Specs {
		if spec, ok := declaration.(*ast.TypeSpec); ok {
			specs = append(specs, spec)
		}
	}
	return specs
}

// hiddenTypeCollector records local hidden definitions reachable from public
// declarations. Their otherwise omitted definitions make consumer-visible
// field, signature, and identity changes observable to the release baseline.
type hiddenTypeCollector struct {
	fset        *token.FileSet
	typeInfo    *types.Info
	qualifier   types.Qualifier
	hiddenTypes map[types.Object]*ast.TypeSpec
	seen        map[types.Object]struct{}
	seenTypes   map[types.Type]struct{}
	entries     []string
}

func (collector *hiddenTypeCollector) collect(declaration ast.Decl) {
	switch declaration := declaration.(type) {
	case *ast.GenDecl:
		switch declaration.Tok {
		case token.TYPE:
			for _, spec := range typeDeclarationSpecs(declaration) {
				if ast.IsExported(spec.Name.Name) {
					collector.visitTypeSpec(spec)
				}
			}
		case token.CONST, token.VAR:
			for _, declaration := range declaration.Specs {
				valueSpec, ok := declaration.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range valueSpec.Names {
					if ast.IsExported(name.Name) {
						collector.visitTypeOf(name)
					}
				}
			}
		}
	case *ast.FuncDecl:
		if declaration.Name == nil || !ast.IsExported(declaration.Name.Name) {
			return
		}
		collector.visitExpression(declaration.Type)
		if declaration.Recv != nil {
			collector.visitExpression(declaration.Recv)
		}
	}
}

func (collector *hiddenTypeCollector) visitTypeSpec(spec *ast.TypeSpec) {
	if spec.TypeParams != nil {
		collector.visitExpression(spec.TypeParams)
	}
	switch typ := spec.Type.(type) {
	case *ast.StructType:
		if typ.Fields == nil {
			return
		}
		for _, field := range typ.Fields.List {
			if len(field.Names) == 0 || len(exportedFieldNames(field.Names)) > 0 {
				collector.visitExpression(field.Type)
			}
		}
	case *ast.InterfaceType:
		if typ.Methods != nil {
			collector.visitExpression(typ.Methods)
		}
	default:
		collector.visitExpression(spec.Type)
	}
}

func (collector *hiddenTypeCollector) visitExpression(expression ast.Node) {
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		object := collector.typeInfo.Defs[identifier]
		if object == nil {
			object = collector.typeInfo.Uses[identifier]
		}
		spec, exists := collector.hiddenTypes[object]
		if !exists || ast.IsExported(spec.Name.Name) {
			return true
		}
		collector.visitSpec(spec)
		return true
	})
}

func (collector *hiddenTypeCollector) visitTypeOf(identifier *ast.Ident) {
	if object := collector.typeInfo.Defs[identifier]; object != nil {
		collector.visitType(object.Type())
	}
}

func (collector *hiddenTypeCollector) visitType(typ types.Type) {
	if typ == nil {
		return
	}
	if _, exists := collector.seenTypes[typ]; exists {
		return
	}
	collector.seenTypes[typ] = struct{}{}
	if alias, ok := typ.(interface{ Rhs() types.Type }); ok {
		collector.visitType(alias.Rhs())
		return
	}
	switch typ := typ.(type) {
	case *types.Named:
		if spec, exists := collector.hiddenTypes[typ.Obj()]; exists && !ast.IsExported(spec.Name.Name) {
			collector.visitSpec(spec)
		}
		for index := 0; index < typ.TypeArgs().Len(); index++ {
			collector.visitType(typ.TypeArgs().At(index))
		}
	case *types.Pointer:
		collector.visitType(typ.Elem())
	case *types.Slice:
		collector.visitType(typ.Elem())
	case *types.Array:
		collector.visitType(typ.Elem())
	case *types.Map:
		collector.visitType(typ.Key())
		collector.visitType(typ.Elem())
	case *types.Chan:
		collector.visitType(typ.Elem())
	case *types.Signature:
		collector.visitTuple(typ.Params())
		collector.visitTuple(typ.Results())
		collector.visitTypeParameters(typ.TypeParams())
	case *types.Struct:
		for index := 0; index < typ.NumFields(); index++ {
			field := typ.Field(index)
			if field.Embedded() || field.Exported() {
				collector.visitType(field.Type())
			}
		}
	case *types.Interface:
		typ.Complete()
		for index := 0; index < typ.NumEmbeddeds(); index++ {
			collector.visitType(typ.EmbeddedType(index))
		}
		for index := 0; index < typ.NumExplicitMethods(); index++ {
			collector.visitType(typ.ExplicitMethod(index).Type())
		}
	case *types.TypeParam:
		collector.visitType(typ.Constraint())
	case *types.Union:
		for index := 0; index < typ.Len(); index++ {
			collector.visitType(typ.Term(index).Type())
		}
	}
}

func (collector *hiddenTypeCollector) visitTuple(tuple *types.Tuple) {
	if tuple == nil {
		return
	}
	for index := 0; index < tuple.Len(); index++ {
		collector.visitType(tuple.At(index).Type())
	}
}

func (collector *hiddenTypeCollector) visitTypeParameters(parameters *types.TypeParamList) {
	if parameters == nil {
		return
	}
	for index := 0; index < parameters.Len(); index++ {
		collector.visitType(parameters.At(index).Constraint())
	}
}

func (collector *hiddenTypeCollector) visitSpec(spec *ast.TypeSpec) {
	object := collector.typeInfo.Defs[spec.Name]
	if object == nil {
		return
	}
	if _, exists := collector.seen[object]; exists {
		return
	}
	collector.seen[object] = struct{}{}
	collector.entries = append(collector.entries, typeDeclarationEntries(collector.fset, spec, collector.typeInfo, collector.qualifier)...)
	collector.visitTypeSpec(spec)
}

func publicAPIEntriesForDeclaration(fset *token.FileSet, declaration ast.Decl, typeInfo *types.Info, qualifier types.Qualifier, privateInterfaceMethods map[string]struct{}) ([]string, error) {
	switch declaration := declaration.(type) {
	case *ast.GenDecl:
		return publicAPIGeneralDeclarationEntries(fset, declaration, typeInfo, qualifier)
	case *ast.FuncDecl:
		return publicAPIFunctionDeclarationEntry(fset, declaration, typeInfo, qualifier, privateInterfaceMethods), nil
	default:
		return nil, nil
	}
}

func publicAPIGeneralDeclarationEntries(fset *token.FileSet, declaration *ast.GenDecl, typeInfo *types.Info, qualifier types.Qualifier) ([]string, error) {
	switch declaration.Tok {
	case token.CONST, token.VAR:
		return publicAPIValueDeclarationEntries(declaration, typeInfo, qualifier)
	case token.TYPE:
		return publicAPITypeDeclarationEntries(fset, declaration, typeInfo, qualifier), nil
	default:
		return nil, nil
	}
}

func publicAPIValueDeclarationEntries(declaration *ast.GenDecl, typeInfo *types.Info, qualifier types.Qualifier) ([]string, error) {
	label := "var"
	if declaration.Tok == token.CONST {
		label = "const"
	}
	entries := []string{}
	for _, spec := range declaration.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, name := range valueSpec.Names {
			if !ast.IsExported(name.Name) {
				continue
			}
			entry, err := publicValueDeclaration(label, name.Name, typeInfo.Defs[name], qualifier)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func publicAPITypeDeclarationEntries(fset *token.FileSet, declaration *ast.GenDecl, typeInfo *types.Info, qualifier types.Qualifier) []string {
	entries := []string{}
	for _, spec := range declaration.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if ok && ast.IsExported(typeSpec.Name.Name) {
			entries = append(entries, typeDeclarationEntries(fset, typeSpec, typeInfo, qualifier)...)
		}
	}
	return entries
}

func publicAPIFunctionDeclarationEntry(fset *token.FileSet, declaration *ast.FuncDecl, typeInfo *types.Info, qualifier types.Qualifier, privateInterfaceMethods map[string]struct{}) []string {
	if declaration.Name == nil {
		return nil
	}
	signature := typedFuncSignature(typeInfo.Defs[declaration.Name], declaration.Type, fset, qualifier)
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		if !ast.IsExported(declaration.Name.Name) {
			return nil
		}
		return []string{fmt.Sprintf("func %s%s", declaration.Name.Name, signature)}
	}
	receiver, method := exportedLocalMethodReceiver(fset, declaration, typeInfo)
	if !ast.IsExported(declaration.Name.Name) && (method == nil || !exportedLocalReceiver(method) || !privateMethodRequiredByInterface(method, qualifier, privateInterfaceMethods)) {
		return nil
	}
	return []string{fmt.Sprintf("method (%s) %s%s", receiver, declaration.Name.Name, signature)}
}

func exportedLocalMethodReceiver(fset *token.FileSet, declaration *ast.FuncDecl, typeInfo *types.Info) (string, *types.Func) {
	receiver := exprString(fset, declaration.Recv.List[0].Type)
	method, ok := typeInfo.Defs[declaration.Name].(*types.Func)
	if !ok {
		return receiver, nil
	}
	return receiver, method
}

func exportedLocalReceiver(method *types.Func) bool {
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}
	named := receiverNamedType(signature.Recv().Type())
	if named == nil || named.Obj().Pkg() != method.Pkg() {
		return false
	}
	return ast.IsExported(named.Obj().Name())
}

func privateMethodRequiredByInterface(method *types.Func, qualifier types.Qualifier, requirements map[string]struct{}) bool {
	_, required := requirements[privateMethodRequirement(method, qualifier)]
	return required
}

func receiverNamedType(typ types.Type) *types.Named {
	switch typ := typ.(type) {
	case *types.Named:
		return typ
	case *types.Pointer:
		return receiverNamedType(typ.Elem())
	default:
		return nil
	}
}

func formatPublicAPI(modulePath string, exported map[string][]string) string {

	paths := make([]string, 0, len(exported))
	for path := range exported {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var out strings.Builder
	out.WriteString("# Public API inventory\n")
	fmt.Fprintf(&out, "module %s\n\n", modulePath)
	for _, path := range paths {
		fmt.Fprintf(&out, "[%s]\n", path)
		for _, entry := range exported[path] {
			out.WriteString(entry)
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

func targetSizes(goarch string) types.Sizes {
	if goarch == "" {
		return nil
	}
	return types.SizesFor("gc", goarch)
}

func packageDirectory(pkgs []goListPackage) string {
	for _, pkg := range pkgs {
		if pkg.Module != nil && pkg.Module.Main && pkg.Module.Dir != "" {
			return pkg.Module.Dir
		}
	}
	if len(pkgs) > 0 {
		return pkgs[0].Dir
	}
	return mustRepoRoot()
}

func targetPackageImporter(ctx context.Context, fset *token.FileSet, directory, goBinary, goos, goarch string) types.Importer {
	if goos == "" {
		return importer.Default()
	}
	return importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		output, err := execCommandInDirEnv(ctx, directory, goBinary, []string{"GOOS=" + goos, "GOARCH=" + goarch}, "list", "-export", "-json", path)
		if err != nil {
			return nil, err
		}
		var pkg struct {
			Export string
		}
		if err := json.Unmarshal(output, &pkg); err != nil {
			return nil, err
		}
		if pkg.Export == "" {
			return nil, fmt.Errorf("target package %s has no export data", path)
		}
		return os.Open(pkg.Export)
	})
}

func publicValueDeclaration(label, name string, object types.Object, qualifier types.Qualifier) (string, error) {
	if object == nil {
		return "", fmt.Errorf("missing type information for exported %s %s", label, name)
	}

	typeName := canonicalTypeString(object.Type(), qualifier)
	switch object := object.(type) {
	case *types.Var:
		return fmt.Sprintf("var %s %s", name, typeName), nil
	case *types.Const:
		return fmt.Sprintf("const %s %s = %s", name, typeName, object.Val().ExactString()), nil
	default:
		return "", fmt.Errorf("unexpected type information for exported %s %s", label, name)
	}
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

	tooling, err := renderToolingModules(ctx)
	if err != nil {
		return dependencyInventory{}, err
	}
	return dependencyInventory{
		ModulePath:      modulePath,
		GoVersion:       readGoVersion(),
		Packages:        packages,
		ExternalModules: modules,
		ToolingModules:  tooling,
	}, nil
}

func renderToolingModules(ctx context.Context) ([]toolingModule, error) {
	const directory = "scripts/rigor/workflow-guard"
	deps, err := listPackagesInDir(ctx, directory, true)
	if err != nil {
		return nil, err
	}
	modulePath, err := modulePathFor(deps)
	if err != nil {
		return nil, err
	}
	seen := map[string]moduleInfo{}
	for _, dep := range deps {
		if dep.Standard || dep.Module == nil || dep.Module.Main || dep.Module.Path == modulePath {
			continue
		}
		seen[dep.Module.Path] = moduleInfo{Path: dep.Module.Path, Version: dep.Module.Version}
	}
	modules := make([]moduleInfo, 0, len(seen))
	for _, module := range seen {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	return []toolingModule{{Path: directory, ModulePath: modulePath, ExternalModules: modules}}, nil
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
		ToolingModules:  dependencyContent.ToolingModules,
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
			if strings.HasPrefix(imp, modulePath+scriptsPackageSuffix) && !strings.HasPrefix(pkg.ImportPath, modulePath+scriptsPackageSuffix) {
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
	return listPackagesInDir(ctx, mustRepoRoot(), withDeps)
}

func listPackagesInDir(ctx context.Context, directory string, withDeps bool) ([]goListPackage, error) {
	return listPackagesInDirForTarget(ctx, directory, withDeps, "", "")
}

func listPackagesInDirForTarget(ctx context.Context, directory string, withDeps bool, goos, goarch string) ([]goListPackage, error) {
	args := []string{"list", "-e", "-json"}
	if withDeps {
		args = append(args, "-deps")
	}
	args = append(args, "./...")

	environment := []string(nil)
	if goos != "" {
		environment = []string{"GOOS=" + goos, "GOARCH=" + goarch}
	}
	output, err := execCommandInDirEnv(ctx, directory, rigorGoBinary(), environment, args...)
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

func rigorGoBinary() string {
	if selected := os.Getenv("STAVE_RIGOR_GO"); selected != "" {
		return selected
	}
	return "go"
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
	return execCommandInDir(ctx, mustRepoRoot(), name, args...)
}

func execCommandInDir(ctx context.Context, directory, name string, args ...string) ([]byte, error) {
	return execCommandInDirEnv(ctx, directory, name, nil, args...)
}

func execCommandInDirEnv(ctx context.Context, directory, name string, environment []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = directory
	if len(environment) > 0 {
		cmd.Env = append(os.Environ(), environment...)
	}
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

func typedFuncSignature(object types.Object, fallback *ast.FuncType, fset *token.FileSet, qualifier types.Qualifier) string {
	if object != nil {
		if signature, ok := object.Type().(*types.Signature); ok {
			return typeSignature(signature, qualifier)
		}
	}
	return astFuncSignature(fset, fallback)
}

func astFuncSignature(fset *token.FileSet, fn *ast.FuncType) string {
	var out strings.Builder
	if fn.TypeParams != nil {
		out.WriteString(typeParameterList(fset, fn.TypeParams))
	}
	out.WriteString(signatureFields(fset, fn.Params))
	out.WriteString(signatureResults(fset, fn.Results))
	return out.String()
}

func typeSignature(signature *types.Signature, qualifier types.Qualifier) string {
	var out strings.Builder
	if parameters := signature.TypeParams(); parameters != nil && parameters.Len() > 0 {
		values := make([]string, 0, parameters.Len())
		for i := 0; i < parameters.Len(); i++ {
			parameter := parameters.At(i)
			values = append(values, parameter.Obj().Name()+" "+canonicalTypeString(parameter.Constraint(), qualifier))
		}
		out.WriteString("[" + strings.Join(values, ", ") + "]")
	}
	out.WriteString(typeTuple(signature.Params(), signature.Variadic(), qualifier))
	results := typeTuple(signature.Results(), false, qualifier)
	if results != "()" {
		if signature.Results().Len() == 1 {
			out.WriteString(" " + results[1:len(results)-1])
		} else {
			out.WriteString(" " + results)
		}
	}
	return out.String()
}

func typeTuple(values *types.Tuple, variadic bool, qualifier types.Qualifier) string {
	if values == nil || values.Len() == 0 {
		return "()"
	}
	items := make([]string, 0, values.Len())
	for i := 0; i < values.Len(); i++ {
		typ := values.At(i).Type()
		if variadic && i == values.Len()-1 {
			if slice, ok := typ.(*types.Slice); ok {
				items = append(items, "..."+canonicalTypeString(slice.Elem(), qualifier))
				continue
			}
		}
		items = append(items, canonicalTypeString(typ, qualifier))
	}
	return "(" + strings.Join(items, ", ") + ")"
}

func canonicalTypeString(typ types.Type, qualifier types.Qualifier) string {
	if declaration, ok := structuredCanonicalTypeString(typ, qualifier); ok {
		return declaration
	}
	return types.TypeString(typ, qualifier)
}

func structuredCanonicalTypeString(typ types.Type, qualifier types.Qualifier) (string, bool) {
	switch typ := typ.(type) {
	case *types.Signature:
		return "func" + typeSignature(typ, qualifier), true
	case *types.Pointer:
		return "*" + canonicalTypeString(typ.Elem(), qualifier), true
	case *types.Slice:
		return "[]" + canonicalTypeString(typ.Elem(), qualifier), true
	case *types.Array:
		return fmt.Sprintf("[%d]%s", typ.Len(), canonicalTypeString(typ.Elem(), qualifier)), true
	case *types.Map:
		return "map[" + canonicalTypeString(typ.Key(), qualifier) + "]" + canonicalTypeString(typ.Elem(), qualifier), true
	case *types.Chan:
		return canonicalChannelType(typ, qualifier), true
	case *types.Struct:
		return canonicalStructType(typ, qualifier), true
	case *types.Interface:
		return canonicalInterfaceType(typ, qualifier), true
	case *types.Union:
		return canonicalUnionType(typ, qualifier), true
	case *types.Named:
		return canonicalNamedType(typ, qualifier)
	}
	return "", false
}

func canonicalChannelType(typ *types.Chan, qualifier types.Qualifier) string {
	prefix := "chan "
	switch typ.Dir() {
	case types.SendOnly:
		prefix = "chan<- "
	case types.RecvOnly:
		prefix = "<-chan "
	}
	element := canonicalTypeString(typ.Elem(), qualifier)
	if typ.Dir() == types.SendRecv {
		if channel, ok := typ.Elem().(*types.Chan); ok && channel.Dir() == types.RecvOnly {
			element = "(" + element + ")"
		}
	}
	return prefix + element
}

func canonicalNamedType(typ *types.Named, qualifier types.Qualifier) (string, bool) {
	arguments := typ.TypeArgs()
	if arguments.Len() == 0 {
		return "", false
	}
	values := make([]string, 0, arguments.Len())
	for index := 0; index < arguments.Len(); index++ {
		values = append(values, canonicalTypeString(arguments.At(index), qualifier))
	}
	name := typ.Obj().Name()
	if pkg := typ.Obj().Pkg(); pkg != nil {
		if prefix := qualifier(pkg); prefix != "" {
			name = prefix + "." + name
		}
	}
	return name + "[" + strings.Join(values, ", ") + "]", true
}

func canonicalStructType(typ *types.Struct, qualifier types.Qualifier) string {
	fields := make([]string, 0, typ.NumFields())
	for index := 0; index < typ.NumFields(); index++ {
		field := typ.Field(index)
		declaration := ""
		if !field.Embedded() {
			name := field.Name()
			if !field.Exported() && field.Pkg() != nil {
				if prefix := qualifier(field.Pkg()); prefix != "" {
					name = prefix + "." + name
				}
			}
			declaration = name + " "
		}
		declaration += canonicalTypeString(field.Type(), qualifier)
		if tag := typ.Tag(index); tag != "" {
			declaration += " " + strconv.Quote(tag)
		}
		fields = append(fields, declaration)
	}
	return "struct{" + strings.Join(fields, "; ") + "}"
}

func canonicalInterfaceType(typ *types.Interface, qualifier types.Qualifier) string {
	typ.Complete()
	if typ.NumEmbeddeds() == 0 && typ.NumExplicitMethods() == 0 {
		// Go 1.22 prints this type as interface{}, while newer toolchains may
		// preserve the any alias. Keep the inventory independent of toolchain.
		return "any"
	}
	elements := make([]string, 0, typ.NumEmbeddeds()+typ.NumExplicitMethods())
	for index := 0; index < typ.NumEmbeddeds(); index++ {
		elements = append(elements, canonicalTypeString(typ.EmbeddedType(index), qualifier))
	}
	for index := 0; index < typ.NumExplicitMethods(); index++ {
		method := typ.ExplicitMethod(index)
		name := method.Name()
		if !method.Exported() && method.Pkg() != nil {
			if prefix := qualifier(method.Pkg()); prefix != "" {
				name = prefix + "." + name
			}
		}
		if signature, ok := method.Type().(*types.Signature); ok {
			elements = append(elements, name+typeSignature(signature, qualifier))
			continue
		}
		elements = append(elements, name+" "+canonicalTypeString(method.Type(), qualifier))
	}
	sort.Strings(elements)
	return "interface{" + strings.Join(elements, "; ") + "}"
}

func canonicalUnionType(typ *types.Union, qualifier types.Qualifier) string {
	terms := make([]string, 0, typ.Len())
	for index := 0; index < typ.Len(); index++ {
		term := typ.Term(index)
		value := canonicalTypeString(term.Type(), qualifier)
		if term.Tilde() {
			value = "~" + value
		}
		terms = append(terms, value)
	}
	sort.Strings(terms)
	return strings.Join(terms, " | ")
}

func packagePathQualifier(current *types.Package) types.Qualifier {
	return func(pkg *types.Package) string {
		if pkg == nil || pkg.Path() == current.Path() {
			return ""
		}
		return pkg.Path()
	}
}

func signatureFields(fset *token.FileSet, fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return "()"
	}
	values := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			values = append(values, signatureType(fset, field.Type))
		}
	}
	return "(" + strings.Join(values, ", ") + ")"
}

func signatureResults(fset *token.FileSet, fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	values := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			values = append(values, signatureType(fset, field.Type))
		}
	}
	if len(values) == 1 {
		return " " + values[0]
	}
	return " (" + strings.Join(values, ", ") + ")"
}

func signatureType(fset *token.FileSet, expression ast.Expr) string {
	if function, ok := expression.(*ast.FuncType); ok {
		return "func" + astFuncSignature(fset, function)
	}
	return exprString(fset, expression)
}

func typeDeclaration(fset *token.FileSet, spec *ast.TypeSpec, typeInfo *types.Info, qualifier types.Qualifier) string {
	var out strings.Builder
	out.WriteString("type ")
	out.WriteString(spec.Name.Name)
	if spec.TypeParams != nil {
		if parameters, ok := typedTypeParameterList(spec.TypeParams, typeInfo, qualifier); ok {
			out.WriteString(parameters)
		} else {
			out.WriteString(typeParameterList(fset, spec.TypeParams))
		}
	}
	if spec.Assign.IsValid() {
		out.WriteString(" =")
	}
	out.WriteByte(' ')
	switch typ := spec.Type.(type) {
	case *ast.StructType:
		out.WriteString("struct { ")
		out.WriteString(strings.Join(publicStructFields(fset, typ.Fields, typeInfo, qualifier), "; "))
		out.WriteString(" }")
	case *ast.InterfaceType:
		out.WriteString("interface { ")
		out.WriteString(strings.Join(publicInterfaceElements(fset, typ.Methods, typeInfo, qualifier), "; "))
		out.WriteString(" }")
	default:
		out.WriteString(canonicalExpressionType(fset, spec.Type, typeInfo, qualifier))
	}
	return out.String()
}

func typeDeclarationEntries(fset *token.FileSet, spec *ast.TypeSpec, typeInfo *types.Info, qualifier types.Qualifier) []string {
	entries := []string{typeDeclaration(fset, spec, typeInfo, qualifier)}
	if comparable, ok := structComparability(spec, typeInfo); ok {
		entries = append(entries, fmt.Sprintf("struct-comparable %s %t", spec.Name.Name, comparable))
	}
	return entries
}

func structComparability(spec *ast.TypeSpec, typeInfo *types.Info) (bool, bool) {
	object := typeInfo.Defs[spec.Name]
	if object == nil || object.Type() == nil {
		return false, false
	}
	if _, ok := object.Type().Underlying().(*types.Struct); !ok {
		return false, false
	}
	return types.Comparable(object.Type()), true
}

func publicStructFields(fset *token.FileSet, fields *ast.FieldList, typeInfo *types.Info, qualifier types.Qualifier) []string {
	if fields == nil {
		return nil
	}
	out := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		names := exportedFieldNames(field.Names)
		if len(field.Names) > 0 && len(names) == 0 {
			continue
		}
		declaration := canonicalExpressionType(fset, field.Type, typeInfo, qualifier)
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

func publicInterfaceElements(fset *token.FileSet, fields *ast.FieldList, typeInfo *types.Info, qualifier types.Qualifier) []string {
	if fields == nil {
		return nil
	}
	out := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		names := make([]string, 0, len(field.Names))
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		declaration := canonicalExpressionType(fset, field.Type, typeInfo, qualifier)
		if fn, ok := field.Type.(*ast.FuncType); ok && len(field.Names) > 0 {
			declaration = typedFuncSignature(typeInfo.Defs[field.Names[0]], fn, fset, qualifier)
		}
		if len(names) > 0 {
			declaration = strings.Join(names, ", ") + declaration
		}
		out = append(out, declaration)
	}
	sort.Strings(out)
	return out
}

func canonicalExpressionType(fset *token.FileSet, expression ast.Expr, typeInfo *types.Info, qualifier types.Qualifier) string {
	if value, ok := typeInfo.Types[expression]; ok && value.Type != nil {
		return canonicalTypeString(value.Type, qualifier)
	}
	return exprString(fset, expression)
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

func typedTypeParameterList(fields *ast.FieldList, typeInfo *types.Info, qualifier types.Qualifier) (string, bool) {
	if fields == nil || len(fields.List) == 0 {
		return "", true
	}
	parameters := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			return "", false
		}
		object, ok := typeInfo.Defs[field.Names[0]].(*types.TypeName)
		if !ok {
			return "", false
		}
		parameter, ok := object.Type().(*types.TypeParam)
		if !ok {
			return "", false
		}
		names := make([]string, 0, len(field.Names))
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		parameters = append(parameters, strings.Join(names, ", ")+" "+canonicalTypeString(parameter.Constraint(), qualifier))
	}
	return "[" + strings.Join(parameters, ", ") + "]", true
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
