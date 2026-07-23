package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var outPath string

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
	for _, e := range extraArgs {
		abs, err := filepath.Abs(e)
		if err != nil { //+gocover:ignore:block os-level failure, rare
			return err
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		items = append(items, item{path: abs, extra: true})
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

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	fmt.Fprintf(out, "Input file: %s\n", relTo(root, absFile))
	fmt.Fprintf(out, "Total files included: %d\n\n", len(items))
	fmt.Fprintln(out, "Index:")
	for _, it := range items {
		if it.extra {
			fmt.Fprintf(out, "  - %s (extra)\n", relTo(root, it.path))
		} else {
			fmt.Fprintf(out, "  - %s\n", relTo(root, it.path))
		}
	}
	fmt.Fprintln(out, strings.Repeat("=", 70))

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

	fmt.Fprintf(errOut, "OK: %d file(s) written to %s\n", len(items), dest)
	return nil
}
