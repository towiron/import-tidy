package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	internalPrefix string
	importOrder    string
)

type (
	ImportGroup int

	ImportDetails struct {
		Path     string
		Group    ImportGroup
		Name     string
		Position token.Pos
	}

	RecognizeOptions struct {
		FilePath          string
		AstNode           *ast.File
		ImportDeclaration *ast.GenDecl
		ImportsList       []ImportDetails
		TokenSet          *token.FileSet
	}
)

const (
	_STANDARD_LIBRARY ImportGroup = iota
	_EXTERNAL_LIBRARY
	_INTERNAL_LIBRARY
)

// determineImportGroup determines the import group based on the import path.
func determineImportGroup(importPath string) ImportGroup {
	switch {
	case !strings.Contains(importPath, "."):
		return _STANDARD_LIBRARY
	case strings.HasPrefix(importPath, internalPrefix):
		return _INTERNAL_LIBRARY
	}
	return _EXTERNAL_LIBRARY
}

// parseImportOrder parses the user-defined import order.
func parseImportOrder() []ImportGroup {
	var (
		order    []ImportGroup
		orderMap = map[string]ImportGroup{
			"standard": _STANDARD_LIBRARY,
			"external": _EXTERNAL_LIBRARY,
			"internal": _INTERNAL_LIBRARY,
		}
	)

	for _, part := range strings.Split(importOrder, ",") {
		group, exists := orderMap[strings.TrimSpace(part)]
		if exists {
			order = append(order, group)
		}
	}

	if len(order) == 0 {
		return []ImportGroup{_STANDARD_LIBRARY, _EXTERNAL_LIBRARY, _INTERNAL_LIBRARY}
	}
	return order
}

// checkImports checks if the imports in a Go file are correctly ordered and formatted.
func checkImports(filePath string, shouldFix bool) error {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	tokenFileSet := token.NewFileSet()
	astNode, err := parser.ParseFile(tokenFileSet, filePath, fileContent, parser.ParseComments)
	if err != nil {
		return err
	}

	importDecl, imports := extractImports(astNode)
	if importDecl == nil {
		return nil
	}

	hasFormatError := validateImportFormat(imports, tokenFileSet, parseImportOrder())

	if (hasFormatError || shouldFix) && shouldFix {
		return reorganizeImports(RecognizeOptions{
			FilePath:          filePath,
			AstNode:           astNode,
			ImportDeclaration: importDecl,
			ImportsList:       imports,
			TokenSet:          tokenFileSet,
		}, string(fileContent))
	}

	return nil
}

// extractImports extracts import details from the given AST node.
func extractImports(astNode *ast.File) (*ast.GenDecl, []ImportDetails) {
	var importDecl *ast.GenDecl
	var imports []ImportDetails

	for _, decl := range astNode.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			importDecl = genDecl
			for _, spec := range genDecl.Specs {
				importSpec := spec.(*ast.ImportSpec)
				importPath := strings.Trim(importSpec.Path.Value, "\"")
				importName := ""
				if importSpec.Name != nil {
					importName = importSpec.Name.Name
				}
				imports = append(imports, ImportDetails{
					Path:     importPath,
					Group:    determineImportGroup(importPath),
					Name:     importName,
					Position: importSpec.Pos(),
				})
			}
			break
		}
	}
	return importDecl, imports
}

func validateImportFormat(imports []ImportDetails, fset *token.FileSet, orderGroups []ImportGroup) bool {
	if len(imports) <= 1 {
		return false
	}

	groupPositions := make(map[ImportGroup]int)
	for i, group := range orderGroups {
		groupPositions[group] = i
	}

	var hasFormatError bool
	var prevGroup = imports[0].Group

	for i := 1; i < len(imports); i++ {
		curr := imports[i]
		prev := imports[i-1]

		if groupPositions[curr.Group] < groupPositions[prevGroup] {
			hasFormatError = true
		}

		if curr.Group != prev.Group {
			if !hasBlankLineBefore(fset, prev.Position, curr.Position) {
				hasFormatError = true
			}
		} else {
			if hasBlankLineBefore(fset, prev.Position, curr.Position) {
				hasFormatError = true
			}
		}

		prevGroup = curr.Group
	}

	return hasFormatError
}

func hasBlankLineBefore(tokenSet *token.FileSet, previousPos, currentPos token.Pos) bool {
	previousPosition := tokenSet.Position(previousPos)
	currentPosition := tokenSet.Position(currentPos)
	return currentPosition.Line-previousPosition.Line > 1
}

func createNewImportContent(opts RecognizeOptions) (string, error) {
	groupedImports := map[ImportGroup][]ImportDetails{}

	for _, importItem := range opts.ImportsList {
		groupedImports[importItem.Group] = append(groupedImports[importItem.Group], importItem)
	}

	for group := range groupedImports {
		sort.Slice(groupedImports[group], func(i, j int) bool {
			return groupedImports[group][i].Path < groupedImports[group][j].Path
		})
	}

	totalImports := 0
	for _, imports := range groupedImports {
		totalImports += len(imports)
	}

	if totalImports == 1 {
		for _, group := range parseImportOrder() {
			imports, exists := groupedImports[group]
			if exists && len(imports) == 1 {
				importItem := imports[0]
				if importItem.Name != "" {
					return fmt.Sprintf("import %s %q", importItem.Name, importItem.Path), nil
				} else {
					return fmt.Sprintf("import %q", importItem.Path), nil
				}
			}
		}
	}

	var importBuffer bytes.Buffer
	importBuffer.WriteString("import (\n")

	order := parseImportOrder()
	isFirstGroup := true

	for _, group := range order {
		imports, exists := groupedImports[group]
		if !exists || len(imports) == 0 {
			continue
		}

		if !isFirstGroup {
			importBuffer.WriteString("\n")
		}
		isFirstGroup = false

		for _, importItem := range imports {
			if importItem.Name != "" {
				importBuffer.WriteString(fmt.Sprintf("\t%s %q\n", importItem.Name, importItem.Path))
			} else {
				importBuffer.WriteString(fmt.Sprintf("\t%q\n", importItem.Path))
			}
		}
	}

	importBuffer.WriteString(")")
	return importBuffer.String(), nil
}

func reorganizeImports(opts RecognizeOptions, originalContent string) error {
	fileInfo, err := os.Stat(opts.FilePath)
	if err != nil {
		return err
	}
	originalMode := fileInfo.Mode()

	importPos := opts.TokenSet.Position(opts.ImportDeclaration.Pos())
	importEnd := opts.TokenSet.Position(opts.ImportDeclaration.End())

	newImportContent, err := createNewImportContent(opts)
	if err != nil {
		return err
	}

	lines := strings.Split(originalContent, "\n")
	importStartLine := importPos.Line - 1
	importEndLine := importEnd.Line - 1

	var newContent strings.Builder

	for i := 0; i < importStartLine; i++ {
		newContent.WriteString(lines[i])
		newContent.WriteString("\n")
	}

	newContent.WriteString(newImportContent)
	newContent.WriteString("\n")

	for i := importEndLine + 1; i < len(lines); i++ {
		newContent.WriteString(lines[i])
		if i < len(lines)-1 {
			newContent.WriteString("\n")
		}
	}

	finalContent, formatErr := format.Source([]byte(newContent.String()))
	if formatErr != nil {
		finalContent = []byte(newContent.String())
	}

	return os.WriteFile(opts.FilePath, finalContent, originalMode)
}

// processDirectory recursively scans a directory and checks all Go files for import correctness.
func processDirectory(directoryPath string, shouldFix bool) error {
	return filepath.WalkDir(directoryPath, func(filePath string, dirEntry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !dirEntry.IsDir() && strings.HasSuffix(filePath, ".go") {
			return checkImports(filePath, shouldFix)
		}

		return nil
	})
}

func main() {
	flag.StringVar(&internalPrefix, "internal-prefix", "", "Prefix for internal imports (required)")
	flag.StringVar(&importOrder, "import-order", "standard,external,internal", "Comma-separated import order")
	flag.Parse()

	if internalPrefix == "" {
		fmt.Println("Error: --internal-prefix is required")
		os.Exit(1)
	}

	if flag.NArg() < 1 {
		fmt.Println("Error: path to the file or directory is required")
		os.Exit(1)
	}

	targetPath := flag.Arg(0)
	shouldFix := flag.NArg() > 1 && flag.Arg(1) == "--fix"

	fileInfo, err := os.Stat(targetPath)
	if err != nil {
		fmt.Printf("Error: failed to access %s: %v\n", targetPath, err)
		os.Exit(1)
	}

	if fileInfo.IsDir() {
		err = processDirectory(targetPath, shouldFix)
	} else {
		err = checkImports(targetPath, shouldFix)
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
