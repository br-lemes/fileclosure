package cmd

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"
)

func findModuleRoot(start string) (string, error) {
	dir := start
	for {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found starting from %s", start)
		}
		dir = parent
	}
}

func computeClosure(cmd *cobra.Command, absFile string) (files []string, root string, err error) {
	root, err = findModuleRoot(filepath.Dir(absFile))
	if err != nil {
		return nil, "", err
	}

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles |
			packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir:  root,
		Fset: fset,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, "", fmt.Errorf("loading packages: %w", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: some packages had load errors (continuing anyway)")
	}

	byFile := map[string]*ast.File{}
	fileToPkg := map[string]*packages.Package{}

	for _, p := range pkgs {
		for i, astFile := range p.Syntax {
			var name string
			if i < len(p.CompiledGoFiles) {
				name = p.CompiledGoFiles[i]
			} else { //+gocover:ignore:block cgo-only edge case
				name = fset.Position(astFile.Pos()).Filename
			}
			absName, _ := filepath.Abs(name)
			_, exists := byFile[absName]
			if !exists {
				byFile[absName] = astFile
				fileToPkg[absName] = p
			}
		}
	}

	_, ok := byFile[absFile]
	if !ok {
		return nil, "", fmt.Errorf(
			"file %s was not found among the module's loaded packages at %s",
			absFile, root)
	}

	visited := map[string]bool{absFile: true}
	queue := []string{absFile}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		astFile := byFile[cur]
		pkg := fileToPkg[cur]
		if astFile == nil || pkg == nil || pkg.TypesInfo == nil {
			//+gocover:ignore:block unreachable given the invariant above
			continue
		}

		add := func(obj types.Object) {
			if obj == nil {
				return
			}
			pos := obj.Pos()
			if !pos.IsValid() {
				return // builtin (len, error, etc.)
			}
			filename := fset.Position(pos).Filename
			absName, _ := filepath.Abs(filename)
			_, inModule := byFile[absName]
			if !inModule {
				return // stdlib or external dependency symbol: out of scope
			}
			if !visited[absName] {
				visited[absName] = true
				queue = append(queue, absName)
			}
		}

		ast.Inspect(astFile, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				add(pkg.TypesInfo.Uses[node])
			case *ast.SelectorExpr:
				sel, ok := pkg.TypesInfo.Selections[node]
				if ok {
					add(sel.Obj())
				}
			}
			return true
		})
	}

	files = make([]string, 0, len(visited))
	for f := range visited {
		files = append(files, f)
	}
	sort.Strings(files)

	return files, root, nil
}

func relTo(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}
