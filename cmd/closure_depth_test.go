package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeDepthClosure_ModuleNotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 2)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestComputeDepthClosure_FileNotInModule(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"ignored.go": `//go:build ignore

package main

func Ignored() {}
`,
	})

	_, err :=
		computeDepthClosure(testCmd(), filepath.Join(dir, "ignored.go"), 2)
	if err == nil {
		t.Fatal("expected error for a file excluded from the build, got nil")
	}
}

func TestComputeDepthClosure_WithinLimitMatchesFullClosure(t *testing.T) {
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

func B() {}
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		filepath.Join(dir, "main.go"): true,
		filepath.Join(dir, "a.go"):    true,
		filepath.Join(dir, "b.go"):    true,
	}
	if len(dc.fullFiles) != len(want) {
		t.Fatalf("fullFiles = %v, want exactly %v", dc.fullFiles, want)
	}
	for _, f := range dc.fullFiles {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
	if len(dc.sigs) != 0 {
		t.Errorf("expected no signature-only functions, got %v", dc.sigs)
	}
}

func TestComputeDepthClosure_BeyondLimitBecomesSignatureOnly(t *testing.T) {
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

func B() {
	C()
}
`,
		"c.go": `package main

// C should never be reached: B is signature-only past the depth limit, so
// its body is never scanned for further calls.
func C() {}
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantFull := map[string]bool{
		filepath.Join(dir, "main.go"): true,
		filepath.Join(dir, "a.go"):    true,
	}
	if len(dc.fullFiles) != len(wantFull) {
		t.Fatalf("fullFiles = %v, want exactly %v", dc.fullFiles, wantFull)
	}
	for _, f := range dc.fullFiles {
		if !wantFull[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
		if f == filepath.Join(dir, "c.go") {
			t.Fatal("c.go must not be reached past a signature-only function")
		}
	}

	if len(dc.sigs) != 1 || dc.sigs[0].file != filepath.Join(dir, "b.go") {
		t.Fatalf("sigs = %v, want exactly one signature from b.go", dc.sigs)
	}
	if !strings.Contains(dc.sigs[0].text, "func B()") {
		t.Errorf("signature text = %q, want it to declare func B()",
			dc.sigs[0].text)
	}
	if strings.Contains(dc.sigs[0].text, "C()") {
		t.Errorf("signature text leaked the function body: %q",
			dc.sigs[0].text)
	}
}

func TestComputeDepthClosure_SignatureTypeExtracted(t *testing.T) {
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

// B is beyond the depth limit; its signature still needs Config to be
// understandable.
func B() Config {
	return Config{}
}

type Config struct {
	Name string
}
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dc.sigs) != 1 ||
		!strings.Contains(dc.sigs[0].text, "func B() Config") {
		t.Fatalf("sigs = %v, want a signature returning Config", dc.sigs)
	}
	if len(dc.types) != 1 ||
		!strings.Contains(dc.types[0].text, "type Config struct") {
		t.Fatalf("types = %v, want Config extracted standalone", dc.types)
	}
	if dc.types[0].file != filepath.Join(dir, "b.go") {
		t.Errorf("types[0].file = %q, want b.go", dc.types[0].file)
	}
}

func TestComputeDepthClosure_TypeAlreadyInFullFileNotDuplicated(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
}
`,
		"a.go": `package main

// A is within the depth limit, so a.go is included in full - including
// Config's declaration.
func A() Config {
	return Config{}
}

type Config struct {
	Name string
}
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dc.types) != 0 {
		t.Errorf("types = %v, want none - Config is already visible via a.go",
			dc.types)
	}
}

func TestComputeDepthClosure_VarWithinLimitExtractedStandalone(t *testing.T) {
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

// MaxRetries lives in its own file, referenced from A (within limit).
var MaxRetries = 5
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dc.vars) != 1 ||
		!strings.Contains(dc.vars[0].text, "var MaxRetries = 5") {
		t.Fatalf("vars = %v, want MaxRetries extracted standalone", dc.vars)
	}
	for _, f := range dc.fullFiles {
		if f == filepath.Join(dir, "config.go") {
			t.Error("config.go should not be pulled in whole just for one var")
		}
	}
}

func TestComputeDepthClosure_VarBeyondLimitUnreachable(t *testing.T) {
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

// B is beyond the depth limit, so its body (including HiddenConst) is
// never scanned.
func B() {
	_ = HiddenConst
}

const HiddenConst = 99
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dc.vars) != 0 {
		t.Errorf("vars = %v, want none - HiddenConst is only used inside a"+
			" signature-only body", dc.vars)
	}
}

func TestComputeDepthClosure_DepthZero(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
}
`,
		"a.go": `package main

func A() {}
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dc.fullFiles) != 1 ||
		dc.fullFiles[0] != filepath.Join(dir, "main.go") {
		t.Fatalf("fullFiles = %v, want only main.go", dc.fullFiles)
	}
	if len(dc.sigs) != 1 || !strings.Contains(dc.sigs[0].text, "func A()") {
		t.Fatalf("sigs = %v, want a signature for A", dc.sigs)
	}
}

func TestComputeDepthClosure_BlankIdentifierSkipped(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {}
`,
		"a.go": `package main

var _ = 1
const _ = 2
`,
	})

	// Exercises buildObjIndex's blank-identifier skip (var _ / const _).
	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dc.fullFiles) != 1 ||
		dc.fullFiles[0] != filepath.Join(dir, "main.go") {
		t.Fatalf("fullFiles = %v, want only main.go", dc.fullFiles)
	}
}

func TestComputeDepthClosure_StdlibCallSkipped(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
}
`,
		"a.go": `package main

import "fmt"

func A() {
	fmt.Println("hi")
}
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		filepath.Join(dir, "main.go"): true,
		filepath.Join(dir, "a.go"):    true,
	}
	if len(dc.fullFiles) != len(want) {
		t.Fatalf("fullFiles = %v, want exactly %v", dc.fullFiles, want)
	}
	for _, f := range dc.fullFiles {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
	if len(dc.sigs) != 0 {
		t.Errorf("expected no signatures, fmt.Println is out of scope, got %v",
			dc.sigs)
	}
}

func TestComputeDepthClosure_SharedDependencyVisitedOnce(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
	B()
}
`,
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

// Shared is called from both A and B; the second reference must be
// recognized as already settled instead of being processed again.
func Shared() {}
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		filepath.Join(dir, "main.go"):   true,
		filepath.Join(dir, "a.go"):      true,
		filepath.Join(dir, "b.go"):      true,
		filepath.Join(dir, "shared.go"): true,
	}
	if len(dc.fullFiles) != len(want) {
		t.Fatalf("fullFiles = %v, want exactly %v (shared.go once)",
			dc.fullFiles, want)
	}
	for _, f := range dc.fullFiles {
		if !want[f] {
			t.Errorf("unexpected file in closure: %s", f)
		}
	}
}

func TestComputeDepthClosure_VarCandidatePromotedToFullFileNotDuplicated(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
	_ = MaxRetries
}
`,
		"a.go": `package main

func A() {
	B()
}
`,
		"config.go": `package main

// MaxRetries is referenced directly from main.go before config.go is known
// to be included at all. B, declared in the same file, is only reached
// afterwards (via A) - that's what pulls config.go in as a full file, and
// by then MaxRetries must not also be emitted as a standalone var.
var MaxRetries = 5

func B() {}
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dc.vars) != 0 {
		t.Errorf("vars = %v, want none - MaxRetries is already visible via"+
			" config.go", dc.vars)
	}
	want := map[string]bool{
		filepath.Join(dir, "main.go"):   true,
		filepath.Join(dir, "a.go"):      true,
		filepath.Join(dir, "config.go"): true,
	}
	if len(dc.fullFiles) != len(want) {
		t.Fatalf("fullFiles = %v, want exactly %v", dc.fullFiles, want)
	}
}

func TestComputeDepthClosure_SignatureSortingAcrossAndWithinFiles(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	FuncA()
	FuncB()
	FuncC()
}
`,
		"shared.go": `package main

func FuncA() {}

func FuncB() {}
`,
		"other.go": `package main

func FuncC() {}
`,
	})

	// depth 0 forces all three into sigs, exercising sortUnits' comparator
	// both across files and within shared.go.
	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dc.sigs) != 3 {
		t.Fatalf("sigs = %v, want exactly 3 signature-only functions", dc.sigs)
	}
	for i := 1; i < len(dc.sigs); i++ {
		prev, cur := dc.sigs[i-1], dc.sigs[i]
		if prev.file > cur.file {
			t.Fatalf("sigs not sorted by file: %v", dc.sigs)
		}
		if prev.file == cur.file && prev.text > cur.text {
			t.Fatalf("sigs not sorted by text within a file: %v", dc.sigs)
		}
	}
}

func TestComputeDepthClosure_MethodReceiverTypeExtracted(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	A()
}
`,
		"a.go": `package main

func A() {
	g := Greeter{}
	_ = g.Greet()
}
`,
		"greeter.go": `package main

type Greeter struct{}

func (g Greeter) Greet() string { return "hi" }
`,
	})

	dc, err := computeDepthClosure(testCmd(), filepath.Join(dir, "main.go"), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dc.sigs) != 1 ||
		!strings.Contains(dc.sigs[0].text, "func (g Greeter) Greet() string") {
		t.Fatalf("sigs = %v, want Greeter.Greet as a signature", dc.sigs)
	}
	if len(dc.types) != 1 ||
		!strings.Contains(dc.types[0].text, "type Greeter struct") {
		t.Fatalf("types = %v, want Greeter extracted for the receiver",
			dc.types)
	}
}
