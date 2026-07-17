package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const misformattedSrc = `package sample

import (
	"os"
	"fmt"
)
`

func testConfig(fix bool) config {
	return config{
		internalPrefix: "git.example.com/team",
		groupOrder:     []importGroup{standardLibrary, externalLibrary, internalLibrary},
		fix:            fix,
	}
}

func TestDetermineImportGroup(t *testing.T) {
	const internalPrefix = "git.example.com/team"

	tests := []struct {
		path string
		want importGroup
	}{
		{"fmt", standardLibrary},
		{"net/http", standardLibrary},
		{"mycompany/pkg", standardLibrary},
		{"github.com/pkg/errors", externalLibrary},
		{"gopkg.in/yaml.v2", externalLibrary},
		{"git.example.com/team", internalLibrary},
		{"git.example.com/team/pkg", internalLibrary},
		{"git.example.com/teammate/pkg", externalLibrary},
	}

	for _, tt := range tests {
		if got := determineImportGroup(tt.path, internalPrefix); got != tt.want {
			t.Errorf("determineImportGroup(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestParseImportOrder(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		order, err := parseImportOrder("standard,external,internal")
		if err != nil {
			t.Fatal(err)
		}
		want := []importGroup{standardLibrary, externalLibrary, internalLibrary}
		assertOrder(t, order, want)
	})

	t.Run("missing groups are appended", func(t *testing.T) {
		order, err := parseImportOrder("internal")
		if err != nil {
			t.Fatal(err)
		}
		want := []importGroup{internalLibrary, standardLibrary, externalLibrary}
		assertOrder(t, order, want)
	})

	t.Run("duplicates are ignored", func(t *testing.T) {
		order, err := parseImportOrder("standard,standard,external")
		if err != nil {
			t.Fatal(err)
		}
		want := []importGroup{standardLibrary, externalLibrary, internalLibrary}
		assertOrder(t, order, want)
	})

	t.Run("unknown group is an error", func(t *testing.T) {
		_, err := parseImportOrder("standart,external")
		if err == nil {
			t.Fatal("expected error for unknown group name")
		}
	})
}

func assertOrder(t *testing.T, got, want []importGroup) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func runOnFile(t *testing.T, cfg config, src string) (bool, string) {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), "sample.go")
	err := os.WriteFile(filePath, []byte(src), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := checkImports(filePath, cfg)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	return changed, string(content)
}

func TestFixReordersAndGroups(t *testing.T) {
	src := `package sample

import (
	"github.com/pkg/errors"
	"os"

	"git.example.com/team/pkg"
	"fmt"
)
`
	want := `package sample

import (
	"fmt"
	"os"

	"github.com/pkg/errors"

	"git.example.com/team/pkg"
)
`
	changed, got := runOnFile(t, testConfig(true), src)
	if !changed {
		t.Fatal("expected file to be reported as changed")
	}
	if got != want {
		t.Errorf("fixed content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFixPreservesComments(t *testing.T) {
	src := `package sample

import (
	"github.com/pkg/errors"
	// doc comment for fmt
	"fmt" // trailing comment
	alias "os"
)
`
	changed, got := runOnFile(t, testConfig(true), src)
	if !changed {
		t.Fatal("expected file to be reported as changed")
	}
	for _, snippet := range []string{"// doc comment for fmt", "// trailing comment", `alias "os"`} {
		if !strings.Contains(got, snippet) {
			t.Errorf("fixed content lost %q:\n%s", snippet, got)
		}
	}
}

func TestFixMergesMultipleImportDecls(t *testing.T) {
	src := `package sample

import "os"

import (
	"fmt"

	"github.com/pkg/errors"
)
`
	want := `package sample

import (
	"fmt"
	"os"

	"github.com/pkg/errors"
)
`
	changed, got := runOnFile(t, testConfig(true), src)
	if !changed {
		t.Fatal("expected file to be reported as changed")
	}
	if got != want {
		t.Errorf("fixed content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFixCustomOrderKeepsAllImports(t *testing.T) {
	order, err := parseImportOrder("standard,internal")
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(true)
	cfg.groupOrder = order

	src := `package sample

import (
	"fmt"

	"github.com/pkg/errors"

	"git.example.com/team/pkg"
)
`
	_, got := runOnFile(t, cfg, src)
	if !strings.Contains(got, `"github.com/pkg/errors"`) {
		t.Fatalf("external import was dropped:\n%s", got)
	}
}

func TestCheckReportsWithoutRewriting(t *testing.T) {
	changed, got := runOnFile(t, testConfig(false), misformattedSrc)
	if !changed {
		t.Fatal("expected misformatted file to be reported")
	}
	if got != misformattedSrc {
		t.Error("check mode must not modify the file")
	}
}

func TestCleanFileIsUntouched(t *testing.T) {
	src := `package sample

import (
	"fmt"
	"os"

	"github.com/pkg/errors"

	"git.example.com/team/pkg"
)
`
	changed, got := runOnFile(t, testConfig(true), src)
	if changed {
		t.Error("clean file must not be reported as changed")
	}
	if got != src {
		t.Error("clean file must not be modified")
	}
}

func TestSingleImportIsUntouched(t *testing.T) {
	src := `package sample

import "fmt"
`
	changed, got := runOnFile(t, testConfig(true), src)
	if changed {
		t.Error("single import must not be reported as changed")
	}
	if got != src {
		t.Error("single import file must not be modified")
	}
}

func TestFileWithoutImports(t *testing.T) {
	src := `package sample

func f() {}
`
	changed, got := runOnFile(t, testConfig(true), src)
	if changed {
		t.Error("file without imports must not be reported as changed")
	}
	if got != src {
		t.Error("file without imports must not be modified")
	}
}

func TestValidateFlagsUnsortedGroup(t *testing.T) {
	changed, _ := runOnFile(t, testConfig(false), misformattedSrc)
	if !changed {
		t.Error("alphabetically unsorted group must be flagged")
	}
}

func TestValidateAllowsCommentBetweenImports(t *testing.T) {
	src := `package sample

import (
	"fmt"
	// os gives access to process arguments
	"os"
)
`
	changed, _ := runOnFile(t, testConfig(false), src)
	if changed {
		t.Error("comment between imports of the same group must not be flagged")
	}
}

func TestProcessDirectorySkipsVendor(t *testing.T) {
	dir := t.TempDir()
	err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o750)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "vendor", "v.go"), []byte(misformattedSrc), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(misformattedSrc), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	flagged, err := processDirectory(dir, testConfig(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(flagged) != 1 || filepath.Base(flagged[0]) != "main.go" {
		t.Errorf("flagged = %v, want only main.go", flagged)
	}
}
