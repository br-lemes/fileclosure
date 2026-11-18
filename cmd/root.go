package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var outPath string
var depthLimit int

var rootCmd = &cobra.Command{
	RunE:  rootRun,
	Args:  cobra.MinimumNArgs(1),
	Use:   "fileclosure <files...>",
	Short: "Concatenate files, resolving the Go dependency closure",
	Long: `Concatenate files, resolving the Go dependency closure

Arguments:
  files   Go files pull in their dependency closure; others are included as-is`,
}

func Execute(version string) error {
	rootCmd.Version = version
	return rootCmd.Execute()
}

func init() {
	rootCmd.Flags().StringVarP(&outPath, "output", "o", "",
		"output file (default: stdout)")
	rootCmd.Flags().IntVarP(&depthLimit, "depth", "d", -1,
		"limit dependency depth; functions first reached beyond this depth are"+
			" included as signatures only, not full files (default: unlimited)")
}

type item struct {
	literal bool
	path    string
}

func rootRun(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	var goArgs, otherArgs []string
	for _, a := range args {
		if filepath.Ext(a) == ".go" {
			goArgs = append(goArgs, a)
		} else {
			otherArgs = append(otherArgs, a)
		}
	}

	absGoFiles := make([]string, 0, len(goArgs))
	for _, g := range goArgs {
		abs, err := filepath.Abs(g)
		if err != nil { //+gocover:ignore:block only fails if cwd is gone
			return err
		}
		absGoFiles = append(absGoFiles, abs)
	}

	dest := "stdout"
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		cmd.SetOut(f)
		dest = outPath
	}

	if len(absGoFiles) == 0 {
		return runConcatOnly(cmd, otherArgs, dest)
	}
	if depthLimit >= 0 {
		return runDepthLimited(cmd, absGoFiles, otherArgs, dest)
	}
	return runUnlimited(cmd, absGoFiles, otherArgs, dest)
}

func runConcatOnly(cmd *cobra.Command, args []string, dest string) error {
	items, err := resolveItems(args, map[string]bool{}, false)
	if err != nil {
		//+gocover:ignore:block downstream of an already-ignored rare failure
		return err
	}

	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.path
	}
	root := commonRoot(paths)

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	fmt.Fprintf(out, "Total files included: %d\n\n", len(items))
	writeIndex(out, root, items)
	writeFullFiles(out, errOut, root, items)

	fmt.Fprintf(errOut, "OK: %d file(s) written to %s\n", len(items), dest)
	return nil
}

func commonRoot(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	sep := string(filepath.Separator)
	common := strings.Split(filepath.Clean(filepath.Dir(paths[0])), sep)
	for _, p := range paths[1:] {
		dir := strings.Split(filepath.Clean(filepath.Dir(p)), sep)
		n := len(common)
		if len(dir) < n {
			n = len(dir)
		}
		i := 0
		for i < n && common[i] == dir[i] {
			i++
		}
		common = common[:i]
	}
	root := strings.Join(common, sep)
	if root == "" && strings.HasPrefix(paths[0], sep) {
		// Join of an empty prefix collapses to "", losing the leading
		// separator when the only shared ancestor is the fs root itself
		root = sep
	}
	return root
}

func runDepthLimited(cmd *cobra.Command, absGoFiles, otherArgs []string, dest string) error {
	dc, err := computeDepthClosure(cmd, absGoFiles, depthLimit)
	if err != nil {
		return err
	}

	seen := map[string]bool{}
	items := make([]item, 0, len(dc.fullFiles)+len(otherArgs)+len(dc.literal))
	for _, f := range dc.fullFiles {
		items = append(items, item{path: f})
		seen[f] = true
	}
	extraArgs := append(append([]string{}, dc.literal...), otherArgs...)
	otherItems, err := resolveItems(extraArgs, seen, true)
	if err != nil {
		//+gocover:ignore:block downstream of an already-ignored rare failure
		return err
	}
	items = append(items, otherItems...)

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	writeInputFiles(out, dc.root, absGoFiles)
	fmt.Fprintf(out, "Depth limit: %d\n", depthLimit)
	fmt.Fprintf(out, "Full files: %d ", len(items))
	if len(otherItems) > 0 {
		fmt.Fprintf(out, "(of which %d extra/literal) ", len(otherItems))
	}
	fmt.Fprintf(out, "| Signatures: %d | Types: %d | Vars/consts: %d\n\n",
		len(dc.sigs), len(dc.types), len(dc.vars))
	writeIndex(out, dc.root, items)
	writeFullFiles(out, errOut, dc.root, items)

	if len(dc.sigs) > 0 {
		fmt.Fprintf(out, "\n%s\nSIGNATURES (beyond depth limit)\n%s\n\n",
			strings.Repeat("=", 70), strings.Repeat("=", 70))
		for _, s := range dc.sigs {
			fmt.Fprintf(out, "// from %s\n%s\n\n", relTo(dc.root, s.file),
				s.text)
		}
	}

	if len(dc.types) > 0 {
		fmt.Fprintf(out, "\n%s\nTYPES\n%s\n\n",
			strings.Repeat("=", 70), strings.Repeat("=", 70))
		for _, t := range dc.types {
			fmt.Fprintf(out, "// from %s\n%s\n\n", relTo(dc.root, t.file),
				t.text)
		}
	}

	if len(dc.vars) > 0 {
		fmt.Fprintf(out, "\n%s\nVARS/CONSTS\n%s\n\n",
			strings.Repeat("=", 70), strings.Repeat("=", 70))
		for _, v := range dc.vars {
			fmt.Fprintf(out, "// from %s\n%s\n\n", relTo(dc.root, v.file),
				v.text)
		}
	}

	total := len(items) + len(dc.sigs) + len(dc.types) + len(dc.vars)
	fmt.Fprintf(errOut, "OK: %d item(s) written to %s\n", total, dest)
	return nil
}

func runUnlimited(cmd *cobra.Command, absGoFiles, otherArgs []string, dest string) error {
	files, literal, root, err := computeClosure(cmd, absGoFiles)
	if err != nil {
		return err
	}

	seen := map[string]bool{}
	items := make([]item, 0, len(files)+len(otherArgs)+len(literal))
	for _, f := range files {
		items = append(items, item{path: f})
		seen[f] = true
	}
	extraArgs := append(append([]string{}, literal...), otherArgs...)
	otherItems, err := resolveItems(extraArgs, seen, true)
	if err != nil {
		//+gocover:ignore:block downstream of an already-ignored rare failure
		return err
	}
	items = append(items, otherItems...)

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	writeInputFiles(out, root, absGoFiles)
	fmt.Fprintf(out, "Total files included: %d\n\n", len(items))
	writeIndex(out, root, items)
	writeFullFiles(out, errOut, root, items)

	fmt.Fprintf(errOut, "OK: %d file(s) written to %s\n", len(items), dest)
	return nil
}

// tag marks whether these items should be flagged "(non-Go)" in the index;
// false in pure-concatenation mode, where the distinction is meaningless.
func resolveItems(args []string, seen map[string]bool, tag bool) ([]item, error) {
	items := make([]item, 0, len(args))
	for _, a := range args {
		abs, err := filepath.Abs(a)
		if err != nil { //+gocover:ignore:block os-level failure, rare
			return nil, err
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		items = append(items, item{path: abs, literal: tag})
	}
	return items, nil
}

func writeInputFiles(out io.Writer, root string, absFiles []string) {
	if len(absFiles) == 1 {
		fmt.Fprintf(out, "Input file: %s\n", relTo(root, absFiles[0]))
		return
	}
	fmt.Fprintln(out, "Input files:")
	for _, f := range absFiles {
		fmt.Fprintf(out, "  - %s\n", relTo(root, f))
	}
}

func writeFullFiles(out, errOut io.Writer, root string, items []item) {
	for _, it := range items {
		content, err := os.ReadFile(it.path)
		if err != nil {
			fmt.Fprintf(errOut, "warning: could not read %s: %v\n", it.path,
				err)
			continue
		}
		fmt.Fprintf(out, "\n%s\nFILE: %s\n%s\n\n", strings.Repeat("=", 70),
			relTo(root, it.path), strings.Repeat("=", 70))
		out.Write(content)
		fmt.Fprintln(out)
	}
}

func writeIndex(out io.Writer, root string, items []item) {
	fmt.Fprintln(out, "Index:")
	for _, it := range items {
		if it.literal {
			fmt.Fprintf(out, "  - %s (literal)\n", relTo(root, it.path))
		} else {
			fmt.Fprintf(out, "  - %s\n", relTo(root, it.path))
		}
	}
	fmt.Fprintln(out, strings.Repeat("=", 70))
}
