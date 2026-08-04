package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func testCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})
	return cmd
}

func TestFindModuleRoot(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":      "module testmod\n\ngo 1.21\n",
		"pkg/file.go": "package pkg\n",
	})

	got, err := findModuleRoot(filepath.Join(dir, "pkg"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
}

func TestFindModuleRoot_NotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := findModuleRoot(dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestComputeClosure_TransitiveSamePackage(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println("start")
	Foo()
}
`,
		"helper.go": `package main

func Foo() {
	Bar()
}
`,
		"extra.go": `package main

func Bar() {}
`,
		"unused.go": `package main

func Unused() {}
`,
	})

	files, root, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "main.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}

	want := map[string]bool{
		filepath.Join(dir, "main.go"):   true,
		filepath.Join(dir, "helper.go"): true,
		filepath.Join(dir, "extra.go"):  true,
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want exactly %v", files, want)
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
}

func TestComputeClosure_CrossPackage(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

import "testmod/sub"

func main() {
	sub.Thing()
}
`,
		"sub/sub.go": `package sub

func Thing() {}
`,
	})

	files, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "main.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		filepath.Join(dir, "main.go"):    true,
		filepath.Join(dir, "sub/sub.go"): true,
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want exactly %v", files, want)
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
}

func TestComputeClosure_TestFileEntry(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func Foo() {}

func Unused() {}
`,
		"main_test.go": `package main

import "testing"

func TestFoo(t *testing.T) {
	Foo()
}
`,
	})

	files, root, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "main_test.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}

	want := map[string]bool{
		filepath.Join(dir, "main_test.go"): true,
		filepath.Join(dir, "main.go"):      true,
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want exactly %v", files, want)
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
}

func TestComputeClosure_ExternalTestFileEntry(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"lib/lib.go": `package lib

func Exported() {}

func unused() {}
`,
		"lib/lib_test.go": `package lib_test

import (
	"testing"
	"testmod/lib"
)

func TestExported(t *testing.T) {
	lib.Exported()
}
`,
	})

	files, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "lib/lib_test.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		filepath.Join(dir, "lib/lib_test.go"): true,
		filepath.Join(dir, "lib/lib.go"):      true,
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want exactly %v", files, want)
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
}

func TestFileInModule(t *testing.T) {
	root := "/mod/root"
	cases := []struct {
		path string
		want bool
	}{
		{root, true},
		{root + "/a.go", true},
		{root + "/pkg/b.go", true},
		{"/mod/other/a.go", false},
		{"/elsewhere/a.go", false},
	}
	for _, c := range cases {
		got := fileInModule(root, c.path)
		if got != c.want {
			t.Errorf("fileInModule(%q, %q) = %v, want %v", root, c.path, got, c.want)
		}
	}
}

func TestComputeClosure_FileNotInModule(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"ignored.go": `//go:build ignore

package main

func Ignored() {}
`,
	})

	_, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "ignored.go")})
	if err == nil {
		t.Fatal("expected error for a file excluded from the build, got nil")
	}
}

func TestComputeClosure_ModuleNotFound(t *testing.T) {
	dir := t.TempDir()

	_, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "main.go")})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestComputeClosure_LoadError(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":  "this is not a valid go.mod file at all {{{",
		"main.go": "package main\n\nfunc main() {}\n",
	})

	_, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "main.go")})
	if err == nil {
		t.Fatal("expected error from packages.Load, got nil")
	}
}

func TestComputeClosure_PackageErrorsWarning(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	Undefined()
}
`,
	})

	errOut := &bytes.Buffer{}
	cmd := testCmd()
	cmd.SetErr(errOut)

	_, _, err := computeClosure(cmd, []string{filepath.Join(dir, "main.go")})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !strings.Contains(errOut.String(),
		"warning: some packages had load errors") {
		t.Errorf("expected a warning about package load errors, got: %q",
			errOut.String())
	}
}

func TestComputeClosure_MethodSelector(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	g := Greeter{}
	_ = g.Greet()
}
`,
		"greeter.go": `package main

type Greeter struct{}

func (g Greeter) Greet() string { return "hi" }
`,
	})

	files, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "main.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		filepath.Join(dir, "main.go"):    true,
		filepath.Join(dir, "greeter.go"): true,
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want exactly %v", files, want)
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
}

func TestComputeClosure_BuiltinIdentifierSkipped(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	s := []int{1, 2, 3}
	_ = len(s)
}
`,
	})

	files, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "main.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != filepath.Join(dir, "main.go") {
		t.Fatalf("files = %v, want only main.go", files)
	}
}

func TestComputeClosure_MultipleEntryFiles(t *testing.T) {
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
		"unused.go": "package main\n\nfunc Unused() {}\n",
	})

	files, root, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}

	want := map[string]bool{
		filepath.Join(dir, "a.go"):      true,
		filepath.Join(dir, "b.go"):      true,
		filepath.Join(dir, "shared.go"): true,
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want exactly %v (union of both entries)",
			files, want)
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
}

func TestComputeClosure_MultipleEntryFiles_SharedFileNotDuplicated(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"a.go": `package main

func A() {
	Shared()
}
`,
		"b.go": `package main

func B() {
	Shared()
}
`,
		"shared.go": `package main

// Shared is reachable from both entry points; it must appear once.
func Shared() {}
`,
	})

	files, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, f := range files {
		if f == filepath.Join(dir, "shared.go") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared.go appeared %d times, want exactly once: %v",
			count, files)
	}
}

func TestComputeClosure_MultipleEntryFiles_OneNotInModule(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"a.go":   "package main\n\nfunc A() {}\n",
		"ignored.go": `//go:build ignore

package main

func Ignored() {}
`,
	})

	_, _, err := computeClosure(testCmd(),
		[]string{filepath.Join(dir, "a.go"), filepath.Join(dir, "ignored.go")})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRelTo(t *testing.T) {
	root := "/home/user/project"
	path := "/home/user/project/cmd/root.go"

	got := relTo(root, path)
	want := filepath.Join("cmd", "root.go")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRelTo_Unrelated(t *testing.T) {
	root := "cmd"
	path := "/etc/hosts"

	got := relTo(root, path)
	if got != path {
		t.Fatalf("got %q, want unchanged %q", got, path)
	}
}
