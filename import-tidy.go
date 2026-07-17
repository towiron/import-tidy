// Command import-tidy checks and fixes the grouping and ordering of import
// statements in Go source files.
//
// Imports are split into three groups — standard library, external, and
// internal (matched by -internal-prefix) — sorted alphabetically within each
// group and separated by single blank lines. Multiple import declarations
// are merged into one block; aliases and comments attached to imports are
// preserved.
//
// Usage:
//
//	import-tidy -internal-prefix=<prefix> [-import-order=standard,external,internal] [-fix] <path>...
//
// Without -fix the tool reports files whose imports need reorganizing and
// exits with code 1; with -fix it rewrites them in place.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	exitOK          = 0
	exitIssuesFound = 1
	exitError       = 2
)

type importGroup int

const (
	standardLibrary importGroup = iota
	externalLibrary
	internalLibrary
)

var groupNames = map[string]importGroup{
	"standard": standardLibrary,
	"external": externalLibrary,
	"internal": internalLibrary,
}

type config struct {
	internalPrefix string
	groupOrder     []importGroup
	fix            bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, paths, err := parseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		fprintln(stderr, "Error:", err)

		return exitError
	}

	flagged := make([]string, 0, len(paths))
	for _, target := range paths {
		files, err := processPath(target, cfg)
		if err != nil {
			fprintln(stderr, "Error:", err)

			return exitError
		}
		flagged = append(flagged, files...)
	}

	label := "needs formatting:"
	if cfg.fix {
		label = "fixed:"
	}
	for _, file := range flagged {
		fprintln(stdout, label, file)
	}

	if len(flagged) > 0 && !cfg.fix {
		return exitIssuesFound
	}

	return exitOK
}

func parseArgs(args []string, stderr io.Writer) (config, []string, error) {
	flags := flag.NewFlagSet("import-tidy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	internalPrefix := flags.String("internal-prefix", "", "prefix identifying internal imports (required)")
	importOrder := flags.String("import-order", "standard,external,internal", "comma-separated import group order")
	fix := flags.Bool("fix", false, "rewrite files instead of just reporting issues")

	err := flags.Parse(args)
	if err != nil {
		return config{}, nil, err
	}

	var paths []string
	for _, arg := range flags.Args() {
		if arg == "--fix" || arg == "-fix" {
			*fix = true

			continue
		}
		paths = append(paths, arg)
	}

	if *internalPrefix == "" {
		return config{}, nil, errors.New("-internal-prefix is required")
	}
	if len(paths) == 0 {
		return config{}, nil, errors.New("path to a file or directory is required")
	}

	groupOrder, err := parseImportOrder(*importOrder)
	if err != nil {
		return config{}, nil, fmt.Errorf("invalid -import-order: %w", err)
	}

	return config{
		internalPrefix: *internalPrefix,
		groupOrder:     groupOrder,
		fix:            *fix,
	}, paths, nil
}

func parseImportOrder(spec string) ([]importGroup, error) {
	var order []importGroup
	seen := make(map[importGroup]bool, len(groupNames))

	for part := range strings.SplitSeq(spec, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		group, ok := groupNames[name]
		if !ok {
			return nil, fmt.Errorf("unknown import group %q (valid: standard, external, internal)", name)
		}
		if seen[group] {
			continue
		}
		seen[group] = true
		order = append(order, group)
	}

	for _, group := range []importGroup{standardLibrary, externalLibrary, internalLibrary} {
		if !seen[group] {
			order = append(order, group)
		}
	}

	return order, nil
}

func processPath(target string, cfg config) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return processDirectory(target, cfg)
	}

	changed, err := checkImports(target, cfg)
	if err != nil || !changed {
		return nil, err
	}

	return []string{target}, nil
}

var skippedDirs = map[string]bool{
	"vendor":       true,
	"testdata":     true,
	"node_modules": true,
}

func processDirectory(root string, cfg config) ([]string, error) {
	var flagged []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			name := entry.Name()
			if path != root && (skippedDirs[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		changed, err := checkImports(path, cfg)
		if err != nil {
			return err
		}
		if changed {
			flagged = append(flagged, path)
		}

		return nil
	})

	return flagged, err
}

func checkImports(filePath string, cfg config) (bool, error) {
	file, err := loadSourceFile(filePath, cfg.internalPrefix)
	if err != nil {
		return false, err
	}

	if len(file.decls) == 0 || !file.needsTidy(cfg.groupOrder) {
		return false, nil
	}
	if !cfg.fix {
		return true, nil
	}

	fixed, err := file.tidy(cfg.groupOrder)
	if err != nil {
		return false, err
	}

	err = os.WriteFile(filePath, fixed, file.mode)
	if err != nil {
		return false, err
	}

	return true, nil
}

type sourceFile struct {
	path    string
	content []byte
	mode    fs.FileMode
	fset    *token.FileSet
	decls   []*ast.GenDecl
	imports []importInfo
}

type importInfo struct {
	path      string
	group     importGroup
	name      string
	doc       []string
	comment   string
	startLine int
	endLine   int
}

func loadSourceFile(path, internalPrefix string) (*sourceFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	file := &sourceFile{
		path:    path,
		content: content,
		mode:    info.Mode(),
		fset:    fset,
	}
	for _, decl := range astFile.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		file.decls = append(file.decls, genDecl)
		for _, spec := range genDecl.Specs {
			importSpec, ok := spec.(*ast.ImportSpec)
			if !ok {
				continue
			}
			file.imports = append(file.imports, newImportInfo(fset, importSpec, internalPrefix))
		}
	}

	return file, nil
}

func newImportInfo(fset *token.FileSet, spec *ast.ImportSpec, internalPrefix string) importInfo {
	importPath := strings.Trim(spec.Path.Value, `"`)

	info := importInfo{
		path:      importPath,
		group:     determineImportGroup(importPath, internalPrefix),
		startLine: fset.Position(spec.Pos()).Line,
		endLine:   fset.Position(spec.End()).Line,
	}
	if spec.Name != nil {
		info.name = spec.Name.Name
	}
	if spec.Doc != nil {
		for _, comment := range spec.Doc.List {
			info.doc = append(info.doc, comment.Text)
		}
		info.startLine = fset.Position(spec.Doc.Pos()).Line
	}
	if spec.Comment != nil && len(spec.Comment.List) > 0 {
		info.comment = spec.Comment.List[0].Text
		info.endLine = fset.Position(spec.Comment.End()).Line
	}

	return info
}

func determineImportGroup(importPath, internalPrefix string) importGroup {
	if importPath == internalPrefix || strings.HasPrefix(importPath, internalPrefix+"/") {
		return internalLibrary
	}

	firstSegment, _, _ := strings.Cut(importPath, "/")
	if strings.Contains(firstSegment, ".") {
		return externalLibrary
	}

	return standardLibrary
}

func (f *sourceFile) needsTidy(order []importGroup) bool {
	if len(f.decls) > 1 {
		return true
	}
	if len(f.imports) <= 1 {
		return false
	}

	position := make(map[importGroup]int, len(order))
	for i, group := range order {
		position[group] = i
	}

	for i := 1; i < len(f.imports); i++ {
		prev, curr := f.imports[i-1], f.imports[i]
		sameGroup := prev.group == curr.group
		blankBetween := curr.startLine-prev.endLine > 1

		switch {
		case position[curr.group] < position[prev.group]:
			return true // groups out of order
		case !sameGroup && !blankBetween:
			return true // missing blank line between groups
		case sameGroup && blankBetween:
			return true // stray blank line inside a group
		case sameGroup && prev.path > curr.path:
			return true // not sorted alphabetically
		}
	}

	return false
}

func (f *sourceFile) tidy(order []importGroup) ([]byte, error) {
	insertLine := f.fset.Position(f.decls[0].Pos()).Line

	removed := make(map[int]bool)
	for _, decl := range f.decls {
		start := f.fset.Position(decl.Pos()).Line
		end := f.fset.Position(decl.End()).Line
		for line := start; line <= end; line++ {
			removed[line] = true
		}
	}

	var b strings.Builder
	for i, line := range strings.Split(string(f.content), "\n") {
		lineNo := i + 1
		if lineNo == insertLine {
			b.WriteString(renderImportDecl(f.imports, order))
			b.WriteByte('\n')
		}
		if removed[lineNo] {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("reorganized %s does not format cleanly (file left unchanged): %w", f.path, err)
	}

	return formatted, nil
}

func renderImportDecl(imports []importInfo, order []importGroup) string {
	var b strings.Builder

	if len(imports) == 1 {
		for _, doc := range imports[0].doc {
			b.WriteString(doc)
			b.WriteByte('\n')
		}
		b.WriteString("import ")
		writeImportLine(&b, imports[0])

		return b.String()
	}

	grouped := make(map[importGroup][]importInfo)
	for _, imp := range imports {
		grouped[imp.group] = append(grouped[imp.group], imp)
	}

	b.WriteString("import (\n")
	firstGroup := true
	for _, group := range order {
		specs := grouped[group]
		if len(specs) == 0 {
			continue
		}
		slices.SortFunc(specs, func(a, b importInfo) int {
			return strings.Compare(a.path, b.path)
		})

		if !firstGroup {
			b.WriteByte('\n')
		}
		firstGroup = false

		for _, imp := range specs {
			for _, doc := range imp.doc {
				b.WriteByte('\t')
				b.WriteString(doc)
				b.WriteByte('\n')
			}
			b.WriteByte('\t')
			writeImportLine(&b, imp)
			b.WriteByte('\n')
		}
	}
	b.WriteString(")")

	return b.String()
}

func writeImportLine(b *strings.Builder, imp importInfo) {
	if imp.name != "" {
		b.WriteString(imp.name)
		b.WriteByte(' ')
	}
	b.WriteString(strconv.Quote(imp.path))
	if imp.comment != "" {
		b.WriteByte(' ')
		b.WriteString(imp.comment)
	}
}

func fprintln(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}
