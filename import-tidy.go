package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var internalPrefix string

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
	_FILE_MODE = 0o600

	_STANDARD_LIBRARY ImportGroup = iota
	_EXTERNAL_LIBRARY
	_INTERNAL_LIBRARY
)

func determineImportGroup(importPath string) ImportGroup {
	switch {
	case !strings.Contains(importPath, "."):
		return _STANDARD_LIBRARY
	case strings.HasPrefix(importPath, internalPrefix):
		return _INTERNAL_LIBRARY
	}
	return _EXTERNAL_LIBRARY
}

func checkImports(filePath string, shouldFix bool) error {
	tokenFileSet := token.NewFileSet()
	astNode, err := parser.ParseFile(tokenFileSet, filePath, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	importDecl, imports := extractImports(astNode)
	if importDecl == nil {
		return nil
	}

	hasFormatError := validateImportFormat(imports, tokenFileSet)

	if hasFormatError && shouldFix {
		return reorganizeImports(RecognizeOptions{
			FilePath:          filePath,
			AstNode:           astNode,
			ImportDeclaration: importDecl,
			ImportsList:       imports,
			TokenSet:          tokenFileSet,
		})
	}

	return nil
}

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

func validateImportFormat(imports []ImportDetails, fset *token.FileSet) bool {
	var hasFormatError bool
	var prevGroup ImportGroup = -1

	for i := 1; i < len(imports); i++ {
		curr := imports[i]
		prev := imports[i-1]

		switch {
		case curr.Group != prevGroup:
			if !hasBlankLineBefore(fset, prev.Position, curr.Position) {
				hasFormatError = true
			}
		case curr.Group == prevGroup:
			if hasBlankLineBefore(fset, prev.Position, curr.Position) {
				hasFormatError = true
			}
		}

		if curr.Group < prevGroup {
			hasFormatError = true
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

// TODO: Optimize the code
func reorganizeImports(opts RecognizeOptions) error {
	var standardImports, externalImports, internalImports []ImportDetails

	for _, importItem := range opts.ImportsList {
		switch importItem.Group {
		case _STANDARD_LIBRARY:
			standardImports = append(standardImports, importItem)
		case _EXTERNAL_LIBRARY:
			externalImports = append(externalImports, importItem)
		case _INTERNAL_LIBRARY:
			internalImports = append(internalImports, importItem)
		}
	}

	sort.Slice(standardImports, func(i, j int) bool {
		return standardImports[i].Path < standardImports[j].Path
	})
	sort.Slice(externalImports, func(i, j int) bool {
		return externalImports[i].Path < externalImports[j].Path
	})
	sort.Slice(internalImports, func(i, j int) bool {
		return internalImports[i].Path < internalImports[j].Path
	})

	newImportDeclaration := &ast.GenDecl{
		Tok:    token.IMPORT,
		Lparen: opts.ImportDeclaration.Lparen,
		Rparen: opts.ImportDeclaration.Rparen,
	}

	for _, importItem := range standardImports {
		newImportDeclaration.Specs = append(newImportDeclaration.Specs,
			createImportSpecification(importItem))
	}

	if len(externalImports) > 0 && len(standardImports) > 0 {
		newImportDeclaration.Specs = append(newImportDeclaration.Specs,
			createGroupSeparator())
	}

	for _, importItem := range externalImports {
		newImportDeclaration.Specs = append(newImportDeclaration.Specs,
			createImportSpecification(importItem))
	}

	if len(internalImports) > 0 && (len(standardImports) > 0 || len(externalImports) > 0) {
		newImportDeclaration.Specs = append(newImportDeclaration.Specs,
			createGroupSeparator())
	}

	for _, importItem := range internalImports {
		newImportDeclaration.Specs = append(newImportDeclaration.Specs,
			createImportSpecification(importItem))
	}

	for i, declaration := range opts.AstNode.Decls {
		genDeclaration, isGenDecl := declaration.(*ast.GenDecl)
		if isGenDecl && genDeclaration.Tok == token.IMPORT {
			opts.AstNode.Decls[i] = newImportDeclaration
			break
		}
	}

	var outputBuffer bytes.Buffer
	if err := printer.Fprint(&outputBuffer, opts.TokenSet, opts.AstNode); err != nil {
		return err
	}

	formattedContent := outputBuffer.String()
	formattedContent = strings.ReplaceAll(
		formattedContent,
		"\"IMPORTGROUP_SEPARATOR\"\n",
		"\n",
	)

	finalContent, formatErr := format.Source([]byte(formattedContent))
	if formatErr != nil {
		finalContent = []byte(formattedContent)
	}

	return os.WriteFile(opts.FilePath, finalContent, _FILE_MODE)
}

func createImportSpecification(importDetails ImportDetails) *ast.ImportSpec {
	spec := &ast.ImportSpec{
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: `"` + importDetails.Path + `"`,
		},
	}

	if importDetails.Name != "" {
		spec.Name = &ast.Ident{Name: importDetails.Name}
	}

	return spec
}

func createGroupSeparator() *ast.ImportSpec {
	return &ast.ImportSpec{
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: `"IMPORTGROUP_SEPARATOR"`,
		},
	}
}

func processDirectory(directoryPath string, shouldFix bool) error {
	return filepath.Walk(
		directoryPath,
		func(filePath string, fileInfo os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !fileInfo.IsDir() && strings.HasSuffix(filePath, ".go") {
				return checkImports(filePath, shouldFix)
			}
			return nil
		})
}

func main() {
	flag.StringVar(&internalPrefix,
		"internal-prefix",
		"",
		"Prefix for internal imports (required)")
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

	fmt.Println("done!")
}
