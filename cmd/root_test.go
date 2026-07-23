package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetRootCmd() (out, errOut *bytes.Buffer) {
	outPath = ""
	rootCmd.SilenceUsage = false
	out, errOut = &bytes.Buffer{}, &bytes.Buffer{}
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	return out, errOut
}

func TestRootRun_Stdout(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	Foo()
}
`,
		"helper.go": `package main

func Foo() {}
`,
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "main.go")})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Total files included: 2") {
		t.Errorf("output missing file count:\n%s", got)
	}
	if !strings.Contains(got, "FILE: main.go") ||
		!strings.Contains(got, "FILE: helper.go") {
		t.Errorf("output missing expected FILE headers:\n%s", got)
	}
}

func TestRootRun_OutputFlag(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":  "module testmod\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	outFile := filepath.Join(dir, "context.txt")

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "main.go"), "-o", outFile})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("expected nothing written to the original out buffer, got %q",
			out.String())
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	if !strings.Contains(string(content), "Total files included: 1") {
		t.Errorf("output file missing expected content:\n%s", content)
	}
}

func TestRootRun_ExtraFiles(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":    "module testmod\n\ngo 1.21\n",
		"main.go":   "package main\n\nfunc main() {}\n",
		"README.md": "# testmod\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "main.go"),
		filepath.Join(dir, "README.md"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "README.md (extra)") {
		t.Errorf("index missing extra marker for README.md:\n%s", got)
	}
	if !strings.Contains(got, "FILE: README.md") {
		t.Errorf("output missing README.md content:\n%s", got)
	}
}

func TestRootRun_ExtraFiles_Deduplication(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":  "module testmod\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "main.go"),
		filepath.Join(dir, "main.go"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if strings.Count(got, "FILE: main.go") != 1 {
		t.Errorf("expected main.go to appear exactly once:\n%s", got)
	}
	if !strings.Contains(got, "Total files included: 1") {
		t.Errorf("expected exactly 1 file counted:\n%s", got)
	}
}

func TestRootRun_NonGoFirstArg(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":    "module testmod\n\ngo 1.21\n",
		"README.md": "# testmod\n",
	})

	resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "README.md")})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecute(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":  "module testmod\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})

	resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "main.go")})

	err := Execute("v1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rootCmd.Version != "v1.2.3" {
		t.Errorf("rootCmd.Version = %q, want %q", rootCmd.Version, "v1.2.3")
	}
}

func TestRootRun_ModuleNotFound(t *testing.T) {
	dir := t.TempDir()

	resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "main.go")})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRootRun_OutputFlag_CreateError(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":  "module testmod\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	badOut := filepath.Join(dir, "no-such-dir", "context.txt")

	resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "main.go"), "-o", badOut})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRootRun_UnreadableExtraFile(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":  "module testmod\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	subdir := filepath.Join(dir, "not-a-file")
	err := os.Mkdir(subdir, 0o755)
	if err != nil {
		t.Fatal(err)
	}

	out, errOut := resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "main.go"), subdir})

	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errOut.String(), "warning: could not read") {
		t.Errorf("expected a read warning on stderr, got: %q", errOut.String())
	}
	if strings.Contains(out.String(), "FILE: not-a-file") {
		t.Errorf("directory content leaked into output:\n%s", out.String())
	}
}

func TestRootRun_MissingArgs(t *testing.T) {
	resetRootCmd()

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
