package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func resetRootCmd() (out, errOut *bytes.Buffer) {
	outPath = ""
	depthLimit = -1
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

func TestRootRun_NonGoFiles(t *testing.T) {
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
	if !strings.Contains(got, "README.md (literal)") {
		t.Errorf("index missing literal marker for README.md:\n%s", got)
	}
	if !strings.Contains(got, "FILE: README.md") {
		t.Errorf("output missing README.md content:\n%s", got)
	}
}

func TestRootRun_DuplicateGoEntry_Deduplication(t *testing.T) {
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

func TestRootRun_GoFileAnyPosition(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":    "module testmod\n\ngo 1.21\n",
		"main.go":   "package main\n\nfunc main() {}\n",
		"README.md": "# testmod\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "README.md"),
		filepath.Join(dir, "main.go"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Input file: main.go") {
		t.Errorf("expected main.go to be treated as the Go entry point"+
			" regardless of its position:\n%s", got)
	}
	if !strings.Contains(got, "README.md (literal)") {
		t.Errorf("expected README.md to be marked literal:\n%s", got)
	}
}

func TestRootRun_MultipleGoEntryPoints(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"a.go": `package main

func A() {
	Shared()
}
`,
		"b.go": "package main\n\nfunc B() {}\n",
		"shared.go": `package main

func Shared() {}
`,
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Total files included: 3") {
		t.Errorf("expected a.go, b.go and shared.go:\n%s", got)
	}
	if !strings.Contains(got, "FILE: shared.go") {
		t.Errorf("shared.go should be pulled in via a.go's closure:\n%s", got)
	}
}

func TestRootRun_InputFileHeader_SingleEntry(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":  "module testmod\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "main.go")})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Input file: main.go") {
		t.Errorf("expected singular input file header:\n%s", got)
	}
	if strings.Contains(got, "Input files:") {
		t.Errorf("did not expect the plural header for a single entry:\n%s",
			got)
	}
}

func TestRootRun_InputFileHeader_MultipleEntries(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"a.go":   "package main\n\nfunc A() {}\n",
		"b.go":   "package main\n\nfunc B() {}\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Input files:") {
		t.Errorf("expected the plural input files header:\n%s", got)
	}
	if !strings.Contains(got, "- a.go") || !strings.Contains(got, "- b.go") {
		t.Errorf("expected both entry files listed:\n%s", got)
	}
}

func TestRootRun_ConcatOnly_SingleNonGoFile(t *testing.T) {
	dir := writeModule(t, map[string]string{"README.md": "# testmod\n"})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "README.md")})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Total files included: 1") {
		t.Errorf("output missing file count:\n%s", got)
	}
	if !strings.Contains(got, "FILE: README.md") {
		t.Errorf("output missing README.md content:\n%s", got)
	}
	if strings.Contains(got, "Input file") {
		t.Errorf("concat-only mode should not print an input file header:\n%s",
			got)
	}
	if strings.Contains(got, "(literal)") {
		t.Errorf("concat-only mode should not mark files literal:\n%s", got)
	}
}

func TestRootRun_ConcatOnly_MultipleNonGoFiles(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"README.md":   "# testmod\n",
		"LICENSE.txt": "MIT\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "README.md"),
		filepath.Join(dir, "LICENSE.txt"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Total files included: 2") {
		t.Errorf("output missing file count:\n%s", got)
	}
	if !strings.Contains(got, "FILE: README.md") ||
		!strings.Contains(got, "FILE: LICENSE.txt") {
		t.Errorf("output missing expected FILE headers:\n%s", got)
	}
}

func TestRootRun_ConcatOnly_NoModuleNeeded(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "notes.txt")})

	// No go.mod anywhere above dir: concat-only mode must not attempt to
	// resolve a module, unlike the closure-computing modes.
	execErr := rootCmd.Execute()
	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}
	if !strings.Contains(out.String(), "FILE: notes.txt") {
		t.Errorf("output missing notes.txt content:\n%s", out.String())
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

func TestRootRun_UnreadableNonGoFile(t *testing.T) {
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

func TestRootRun_DepthFlag_SignaturesAndTypesSections(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
}
`,
		"a.go": `package main

func A() {
	B()
}
`,
		"b.go": `package main

func B() Config {
	return Config{}
}

type Config struct {
	Name string
}
`,
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{"--depth", "1", filepath.Join(dir, "main.go")})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Depth limit: 1") {
		t.Errorf("output missing depth limit header:\n%s", got)
	}
	if !strings.Contains(got, "SIGNATURES (beyond depth limit)") ||
		!strings.Contains(got, "func B() Config") {
		t.Errorf("output missing expected signature section:\n%s", got)
	}
	if !strings.Contains(got, "TYPES") ||
		!strings.Contains(got, "type Config struct") {
		t.Errorf("output missing expected types section:\n%s", got)
	}
	if strings.Contains(got, "FILE: b.go") {
		t.Errorf(
			"b.go should not be included in full past the depth limit:\n%s",
			got)
	}
}

func TestRootRun_DepthFlag_NonGoFilesAlwaysFull(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
}
`,
		"a.go": `package main

func A() {
	B()
}
`,
		"b.go":      "package main\n\nfunc B() {}\n",
		"README.md": "# testmod\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		"--depth", "0",
		filepath.Join(dir, "main.go"),
		filepath.Join(dir, "README.md"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "FILE: README.md") ||
		!strings.Contains(got, "README.md (literal)") {
		t.Errorf("non-Go file should always be included in full regardless"+
			" of --depth:\n%s", got)
	}
}

func TestRootRun_DepthFlag_MultipleEntryPoints(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"a.go": `package main

func A() {
	Shared()
}
`,
		"b.go": "package main\n\nfunc B() {}\n",
		"shared.go": `package main

func Shared() {}
`,
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		"--depth", "1",
		filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "FILE: shared.go") {
		t.Errorf("shared.go should be reached within depth 1 of a.go:\n%s",
			got)
	}
}

func TestRootRun_UnlimitedByDefault_NoDepthHeader(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":  "module testmod\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "main.go")})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "Depth limit:") {
		t.Errorf("depth header should not appear when --depth is not set:\n%s",
			got)
	}
}

func TestRootRun_DepthFlag_VarsSection(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
}
`,
		"a.go": `package main

func A() {
	_ = MaxRetries
}
`,
		"config.go": `package main

var MaxRetries = 5
`,
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{"--depth", "1", filepath.Join(dir, "main.go")})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "VARS/CONSTS") ||
		!strings.Contains(got, "var MaxRetries = 5") {
		t.Errorf("output missing expected vars/consts section:\n%s", got)
	}
	if strings.Contains(got, "FILE: config.go") {
		t.Errorf(
			"config.go should not be included in full just for one var:\n%s",
			got)
	}
}

func TestRootRun_DepthFlag_ModuleNotFound(t *testing.T) {
	dir := t.TempDir()

	resetRootCmd()
	rootCmd.SetArgs([]string{"--depth", "1", filepath.Join(dir, "main.go")})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCommonRoot_SameDir(t *testing.T) {
	got := commonRoot([]string{
		filepath.Join("a", "b", "one.txt"),
		filepath.Join("a", "b", "two.txt"),
	})
	want := filepath.Join("a", "b")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommonRoot_DifferentDirs(t *testing.T) {
	got := commonRoot([]string{
		filepath.Join("a", "b", "one.txt"),
		filepath.Join("a", "c", "two.txt"),
	})
	want := "a"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommonRoot_SinglePath(t *testing.T) {
	got := commonRoot([]string{filepath.Join("a", "b", "one.txt")})
	want := filepath.Join("a", "b")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommonRoot_Empty(t *testing.T) {
	got := commonRoot(nil)
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestCommonRoot_ShallowerPathNarrowsRoot(t *testing.T) {
	got := commonRoot([]string{
		filepath.Join("a", "b", "c", "one.txt"),
		filepath.Join("a", "two.txt"),
	})
	want := "a"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommonRoot_AbsolutePaths_OnlyRootShared(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("leading-separator behavior is Unix-specific")
	}

	got := commonRoot([]string{"/a/one.txt", "/b/two.txt"})
	if got != "/" {
		t.Fatalf("got %q, want %q", got, "/")
	}
}

func TestRootRun_NonGoFiles_Deduplication(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":    "module testmod\n\ngo 1.21\n",
		"main.go":   "package main\n\nfunc main() {}\n",
		"README.md": "# testmod\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "main.go"),
		filepath.Join(dir, "README.md"),
		filepath.Join(dir, "README.md"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if strings.Count(got, "FILE: README.md") != 1 {
		t.Errorf("expected README.md to appear exactly once:\n%s", got)
	}
	if !strings.Contains(got, "Total files included: 2") {
		t.Errorf("expected main.go + README.md once, got:\n%s", got)
	}
}

func TestRootRun_ConcatOnly_Deduplication(t *testing.T) {
	dir := writeModule(t, map[string]string{"README.md": "# testmod\n"})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "README.md"),
		filepath.Join(dir, "README.md"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if strings.Count(got, "FILE: README.md") != 1 {
		t.Errorf("expected README.md to appear exactly once:\n%s", got)
	}
	if !strings.Contains(got, "Total files included: 1") {
		t.Errorf("expected exactly 1 file counted:\n%s", got)
	}
}

// testdata/ is skipped by "./..." package matching, so a.go never shows up
// in the loaded packages - this reproduces the original bug report (a .go
// file passed as an argument, but excluded from the module's build), which
// must now succeed as a literal file instead of erroring out.
func TestRootRun_GoFileOutsideModule_TreatedAsLiteral(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                          "module testmod\n\ngo 1.21\n",
		"main.go":                         "package main\n\nfunc main() {}\n",
		filepath.Join("testdata", "a.go"): "package a\n\nfunc A() {}\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		filepath.Join(dir, "main.go"),
		filepath.Join(dir, "testdata", "a.go"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Total files included: 2") {
		t.Errorf("output missing file count:\n%s", got)
	}
	if !strings.Contains(got, "FILE: "+filepath.Join("testdata", "a.go")) {
		t.Errorf("output missing literal file content:\n%s", got)
	}
	if !strings.Contains(got,
		filepath.Join("testdata", "a.go")+" (literal)") {
		t.Errorf("index missing literal marker for the out-of-module .go"+
			" file:\n%s", got)
	}
}

func TestRootRun_DepthFlag_GoFileOutsideModule_TreatedAsLiteral(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                          "module testmod\n\ngo 1.21\n",
		"main.go":                         "package main\n\nfunc main() {}\n",
		filepath.Join("testdata", "a.go"): "package a\n\nfunc A() {}\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{
		"--depth", "1",
		filepath.Join(dir, "main.go"),
		filepath.Join(dir, "testdata", "a.go"),
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "FILE: "+filepath.Join("testdata", "a.go")) {
		t.Errorf("output missing literal file content:\n%s", got)
	}
	if !strings.Contains(got,
		filepath.Join("testdata", "a.go")+" (literal)") {
		t.Errorf("index missing literal marker:\n%s", got)
	}
	if !strings.Contains(got, "(of which 1 extra/literal)") {
		t.Errorf("summary line missing extra/literal count:\n%s", got)
	}
}

// Every .go argument falls outside the module's build: the closure itself
// is empty, but the run must still succeed with the file included
// literally, rather than erroring out.
func TestRootRun_AllGoEntriesOutsideModule(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                          "module testmod\n\ngo 1.21\n",
		filepath.Join("testdata", "a.go"): "package a\n\nfunc A() {}\n",
	})

	out, _ := resetRootCmd()
	rootCmd.SetArgs([]string{filepath.Join(dir, "testdata", "a.go")})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Total files included: 1") {
		t.Errorf("output missing file count:\n%s", got)
	}
	if !strings.Contains(got, "FILE: "+filepath.Join("testdata", "a.go")) {
		t.Errorf("output missing literal file content:\n%s", got)
	}
}
