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

func loadModule(cmd *cobra.Command, absFile string) (fset *token.FileSet, byFile map[string]*ast.File, fileToPkg map[string]*packages.Package, root string, err error) {
	root, err = findModuleRoot(filepath.Dir(absFile))
	if err != nil {
		return nil, nil, nil, "", err
	}

	fset = token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles |
			packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir:  root,
		Fset: fset,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("loading packages: %w", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: some packages had load errors (continuing anyway)")
	}

	byFile = map[string]*ast.File{}
	fileToPkg = map[string]*packages.Package{}

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

	return fset, byFile, fileToPkg, root, nil
}

// Field/method selectors resolve to the field's own object, not its
// struct's type - a type only surfaces if named directly somewhere.
func scanRefs(node ast.Node, info *types.Info) []types.Object {
	var objects []types.Object
	seen := map[types.Object]bool{}
	add := func(obj types.Object) {
		if obj == nil || seen[obj] {
			return
		}
		seen[obj] = true
		objects = append(objects, obj)
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch nd := n.(type) {
		case *ast.Ident:
			add(info.Uses[nd])
		case *ast.SelectorExpr:
			sel, ok := info.Selections[nd]
			if ok {
				add(sel.Obj())
			}
		}
		return true
	})
	return objects
}

func computeClosure(cmd *cobra.Command, absFile string) (files []string, root string, err error) {
	fset, byFile, fileToPkg, root, err := loadModule(cmd, absFile)
	if err != nil {
		return nil, "", err
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

		for _, obj := range scanRefs(astFile, pkg.TypesInfo) {
			pos := obj.Pos()
			if !pos.IsValid() {
				continue // builtin (len, error, etc.)
			}
			filename := fset.Position(pos).Filename
			absName, _ := filepath.Abs(filename)
			_, inModule := byFile[absName]
			if !inModule {
				continue // stdlib or external dependency symbol: out of scope
			}
			if !visited[absName] {
				visited[absName] = true
				queue = append(queue, absName)
			}
		}
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
