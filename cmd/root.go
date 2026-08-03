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
	Use:   "fileclosure <file.go> [files...]",
	Short: "Compute the transitive file-level dependency closure of a Go file",
	Long: `Compute the transitive file-level dependency closure of a Go file

Arguments:
  file.go   Entry point analyzed for the dependency closure.
  files     Extra files included directly, without dependency analysis.`,
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
	extra bool
	path  string
}

func rootRun(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	entryArg, extraArgs := args[0], args[1:]

	if filepath.Ext(entryArg) != ".go" {
		return fmt.Errorf("first argument must be a .go file, got %s", entryArg)
	}

	absFile, err := filepath.Abs(entryArg)
	if err != nil { //+gocover:ignore:block only fails if cwd is gone
		return err
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

	if depthLimit >= 0 {
		return runDepthLimited(cmd, absFile, extraArgs, dest)
	}
	return runUnlimited(cmd, absFile, extraArgs, dest)
}

func runDepthLimited(cmd *cobra.Command, absFile string, extraArgs []string, dest string) error {
	dc, err := computeDepthClosure(cmd, absFile, depthLimit)
	if err != nil {
		return err
	}

	seen := map[string]bool{}
	items := make([]item, 0, len(dc.fullFiles)+len(extraArgs))
	for _, f := range dc.fullFiles {
		items = append(items, item{path: f})
		seen[f] = true
	}
	extraItems, err := resolveExtraItems(extraArgs, seen)
	if err != nil {
		//+gocover:ignore:block downstream of an already-ignored rare failure
		return err
	}
	items = append(items, extraItems...)

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	fmt.Fprintf(out, "Input file: %s\n", relTo(dc.root, absFile))
	fmt.Fprintf(out, "Depth limit: %d\n", depthLimit)
	fmt.Fprintf(out, "Full files: %d ", len(items))
	if len(extraItems) > 0 {
		fmt.Fprintf(out, "(of which %d extra) ", len(extraItems))
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

func runUnlimited(cmd *cobra.Command, absFile string, extraArgs []string, dest string) error {
	files, root, err := computeClosure(cmd, absFile)
	if err != nil {
		return err
	}

	items := make([]item, 0, len(files)+len(extraArgs))
	seen := map[string]bool{}
	for _, f := range files {
		items = append(items, item{path: f})
		seen[f] = true
	}
	extraItems, err := resolveExtraItems(extraArgs, seen)
	if err != nil {
		//+gocover:ignore:block downstream of an already-ignored rare failure
		return err
	}
	items = append(items, extraItems...)

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	fmt.Fprintf(out, "Input file: %s\n", relTo(root, absFile))
	fmt.Fprintf(out, "Total files included: %d\n\n", len(items))
	writeIndex(out, root, items)
	writeFullFiles(out, errOut, root, items)

	fmt.Fprintf(errOut, "OK: %d file(s) written to %s\n", len(items), dest)
	return nil
}

// Always taken as-is, in full, regardless of --depth.
func resolveExtraItems(extraArgs []string, seen map[string]bool) ([]item, error) {
	items := make([]item, 0, len(extraArgs))
	for _, e := range extraArgs {
		abs, err := filepath.Abs(e)
		if err != nil { //+gocover:ignore:block os-level failure, rare
			return nil, err
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		items = append(items, item{path: abs, extra: true})
	}
	return items, nil
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
		if it.extra {
			fmt.Fprintf(out, "  - %s (extra)\n", relTo(root, it.path))
		} else {
			fmt.Fprintf(out, "  - %s\n", relTo(root, it.path))
		}
	}
	fmt.Fprintln(out, strings.Repeat("=", 70))
}
