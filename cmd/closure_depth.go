package cmd

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"sort"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"
)

// - Entry files (.go CLI args): always included in full.
// - Function reached at depth <= limit: whole file included, scanned further.
// - Function reached beyond limit: signature only, body never scanned.
// - Types: always resolved in full wherever referenced (in-limit code or
//   an emitted signature), transitively, no depth cap of their own.
// - Vars/consts: only pulled in via in-limit scanning; never appear in
//   signatures, so unreachable ones beyond the limit are just dropped.
// - Non-Go CLI args: untouched, always whole-file (see root.go).

type depthClosure struct {
	root      string
	fullFiles []string
	literal   []string
	sigs      []depthUnit
	types     []depthUnit
	vars      []depthUnit
}

type depthUnit struct {
	file string
	text string
}

type objIndex struct {
	declFile  map[types.Object]string
	funcDecl  map[types.Object]*ast.FuncDecl
	typeSpec  map[types.Object]*ast.TypeSpec
	valueSpec map[types.Object]*ast.ValueSpec
	valueTok  map[types.Object]token.Token
}

func buildObjIndex(byFile map[string]*ast.File, fileToPkg map[string]*packages.Package) *objIndex {
	idx := &objIndex{
		declFile:  map[types.Object]string{},
		funcDecl:  map[types.Object]*ast.FuncDecl{},
		typeSpec:  map[types.Object]*ast.TypeSpec{},
		valueSpec: map[types.Object]*ast.ValueSpec{},
		valueTok:  map[types.Object]token.Token{},
	}
	for absFile, astFile := range byFile {
		pkg := fileToPkg[absFile]
		if pkg == nil || pkg.TypesInfo == nil {
			//+gocover:ignore:block unreachable given the invariant above
			continue
		}
		for _, decl := range astFile.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				obj := pkg.TypesInfo.Defs[d.Name]
				if obj != nil {
					idx.funcDecl[obj] = d
					idx.declFile[obj] = absFile
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						obj := pkg.TypesInfo.Defs[s.Name]
						if obj != nil {
							idx.typeSpec[obj] = s
							idx.declFile[obj] = absFile
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.Name == "_" {
								continue
							}
							obj := pkg.TypesInfo.Defs[name]
							if obj != nil {
								idx.valueSpec[obj] = s
								idx.valueTok[obj] = d.Tok
								idx.declFile[obj] = absFile
							}
						}
					}
				}
			}
		}
	}
	return idx
}

func filterTypeNames(objects []types.Object) []types.Object {
	var out []types.Object
	for _, o := range objects {
		tn, ok := o.(*types.TypeName)
		if ok {
			out = append(out, tn)
		}
	}
	return out
}

func computeDepthClosure(cmd *cobra.Command, absFiles []string, depthLimit int) (*depthClosure, error) {
	fset, byFile, fileToPkg, root, err := loadModule(cmd, absFiles[0])
	if err != nil {
		return nil, err
	}

	idx := buildObjIndex(byFile, fileToPkg)

	includedFiles := map[string]bool{}
	visited := map[string]bool{}

	type queued struct {
		file  string
		depth int
	}
	var literal []string
	var queue []queued
	for _, absFile := range absFiles {
		_, ok := byFile[absFile]
		if !ok {
			literal = append(literal, absFile)
			continue
		}
		includedFiles[absFile] = true
		visited[absFile] = true
		queue = append(queue, queued{absFile, 0})
	}

	type funcCandidate struct {
		obj      *types.Func
		declFile string
	}
	var funcCandidates []funcCandidate
	funcSeen := map[*types.Func]bool{}

	var rawTypeRefs []types.Object
	typeSeenRaw := map[types.Object]bool{}

	type varCandidate struct {
		obj      types.Object
		declFile string
	}
	var varCandidates []varCandidate
	varSeenRaw := map[types.Object]bool{}

	// Two-phase on purpose: a symbol seen "beyond limit" here might still
	// belong to a file that gets included in full via a shorter path found
	// later in the same BFS. Resolving signatures/types/vars now would risk
	// duplicating that file's content. Phase 2 resolves once includedFiles
	// is final.
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		astFile := byFile[cur.file]
		pkg := fileToPkg[cur.file]
		if astFile == nil || pkg == nil || pkg.TypesInfo == nil {
			//+gocover:ignore:block unreachable given the invariant above
			continue
		}

		for _, ref := range scanRefs(astFile, pkg.TypesInfo) {
			switch obj := ref.(type) {
			case *types.Func:
				declFile, ok := idx.declFile[obj]
				if !ok || visited[declFile] {
					continue // stdlib/external, or file already settled
				}
				nextDepth := cur.depth + 1
				if nextDepth <= depthLimit {
					visited[declFile] = true
					includedFiles[declFile] = true
					queue = append(queue, queued{declFile, nextDepth})
				} else if !funcSeen[obj] {
					funcSeen[obj] = true
					funcCandidates = append(funcCandidates,
						funcCandidate{obj, declFile})
				}
			case *types.TypeName:
				if !typeSeenRaw[obj] {
					typeSeenRaw[obj] = true
					rawTypeRefs = append(rawTypeRefs, obj)
				}
			case *types.Var, *types.Const:
				declFile, ok := idx.declFile[obj]
				if !ok || varSeenRaw[obj] {
					continue // local var/param (not in idx), or already seen
				}
				varSeenRaw[obj] = true
				varCandidates = append(varCandidates,
					varCandidate{obj, declFile})
			}
		}
	}

	sort.Strings(literal)
	res := &depthClosure{root: root, literal: literal}
	for f := range includedFiles {
		res.fullFiles = append(res.fullFiles, f)
	}
	sort.Strings(res.fullFiles)

	typeQueue := append([]types.Object{}, rawTypeRefs...)

	for _, fc := range funcCandidates {
		if includedFiles[fc.declFile] {
			//+gocover:ignore:block unreachable
			// visited[declFile] already filters this in phase 1
			continue
		}
		decl := idx.funcDecl[fc.obj]
		if decl == nil {
			//+gocover:ignore:block unreachable
			// funcDecl/declFile are set together
			continue
		}
		sigDecl := &ast.FuncDecl{
			Recv: decl.Recv, Name: decl.Name, Type: decl.Type}
		sigFile := fc.declFile
		sigText := printNode(fset, sigDecl)
		// A generated templ component is more useful shown as the .templ
		// declaration it came from than as the Go boilerplate signature.
		if src, ok := templSource(fc.declFile); ok {
			if text, ok := templSignature(src, fc.obj.Name()); ok {
				sigFile, sigText = src, text
			}
		}
		res.sigs = append(res.sigs, depthUnit{
			file: sigFile,
			text: sigText,
		})
		pkg := fileToPkg[fc.declFile]
		// Signature-only: scan only the receiver + func type for further
		// type refs, never the body.
		if decl.Recv != nil {
			typeQueue = append(typeQueue,
				filterTypeNames(scanRefs(decl.Recv, pkg.TypesInfo))...)
		}
		typeQueue = append(typeQueue,
			filterTypeNames(scanRefs(decl.Type, pkg.TypesInfo))...)
	}

	for _, vc := range varCandidates {
		if includedFiles[vc.declFile] {
			continue
		}
		spec := idx.valueSpec[vc.obj]
		if spec == nil {
			//+gocover:ignore:block unreachable
			// valueSpec/declFile are set together
			continue
		}
		res.vars = append(res.vars, depthUnit{
			file: vc.declFile,
			text: printValueSpec(fset, idx.valueTok[vc.obj], spec),
		})
		pkg := fileToPkg[vc.declFile]
		typeQueue = append(typeQueue,
			filterTypeNames(scanRefs(spec, pkg.TypesInfo))...)
	}

	typeDone := map[types.Object]bool{}
	for len(typeQueue) > 0 {
		obj := typeQueue[0]
		typeQueue = typeQueue[1:]
		if typeDone[obj] {
			continue
		}
		typeDone[obj] = true
		declFile, ok := idx.declFile[obj]
		if !ok || includedFiles[declFile] {
			continue
		}
		spec := idx.typeSpec[obj]
		if spec == nil {
			//+gocover:ignore:block unreachable
			// typeSpec/declFile are set together
			continue
		}
		res.types = append(res.types, depthUnit{
			file: declFile,
			text: printTypeSpec(fset, spec),
		})
		pkg := fileToPkg[declFile]
		typeQueue = append(typeQueue,
			filterTypeNames(scanRefs(spec, pkg.TypesInfo))...)
	}

	sortUnits(res.sigs)
	sortUnits(res.types)
	sortUnits(res.vars)

	return res, nil
}

func sortUnits(units []depthUnit) {
	sort.Slice(units, func(i, j int) bool {
		if units[i].file != units[j].file {
			return units[i].file < units[j].file
		}
		return units[i].text < units[j].text
	})
}

func printNode(fset *token.FileSet, node ast.Node) string {
	var buf bytes.Buffer
	err := printer.Fprint(&buf, fset, node)
	if err != nil {
		//+gocover:ignore:block printer.Fprint doesn't fail on valid AST nodes
		return fmt.Sprintf("/* error printing node: %v */", err)
	}
	return buf.String()
}

func printTypeSpec(fset *token.FileSet, spec *ast.TypeSpec) string {
	decl := &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{spec}}
	return printNode(fset, decl)
}

// Grouped decls (var ( a = 1; b = 2 )) print only the referenced spec.
func printValueSpec(fset *token.FileSet, tok token.Token, spec *ast.ValueSpec) string {
	decl := &ast.GenDecl{Tok: tok, Specs: []ast.Spec{spec}}
	return printNode(fset, decl)
}
