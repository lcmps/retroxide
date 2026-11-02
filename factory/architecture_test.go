package factory

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestBackendLayerDependencies(t *testing.T) {
	const modulePrefix = "github.com/lcmps/retroxide/"
	allowed := map[string][]string{
		"domain":         {"domain"},
		"application":    {"domain", "application"},
		"infrastructure": {"domain", "application", "infrastructure"},
		"interface":      {"domain", "application", "interface"},
		"factory":        {"domain", "application", "infrastructure", "interface", "factory"},
		"bootstrap":      {"domain", "application", "infrastructure", "factory", "bootstrap"},
	}
	if _, err := os.Stat(filepath.Join("..", "internal")); !os.IsNotExist(err) {
		t.Fatalf("backend must not have an internal directory: %v", err)
	}
	for layer, dependencies := range allowed {
		t.Run(layer, func(t *testing.T) {
			err := filepath.WalkDir(filepath.Join("..", layer), func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				source, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imported := range source.Imports {
					importPath, err := strconv.Unquote(imported.Path.Value)
					if err != nil {
						return err
					}
					if strings.HasPrefix(importPath, modulePrefix) {
						dependency, _, _ := strings.Cut(strings.TrimPrefix(importPath, modulePrefix), "/")
						if !slices.Contains(dependencies, dependency) {
							t.Errorf("%s: %s must not depend on %s", path, layer, importPath)
						}
					} else if layer == "domain" && strings.Contains(strings.Split(importPath, "/")[0], ".") {
						t.Errorf("%s: domain must not depend on external package %s", path, importPath)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
