# fileclosure

Given a list of files, `fileclosure` concatenates them into a single text
file. Any `.go` file is expanded into its full dependency closure within
the same module — the files defining the functions, types, variables, and
constants it uses directly, the files those depend on, and so on. Any
other file is included as-is, no analysis. That includes a `.go` file that
falls outside the module's build: instead of failing, it's included
literally.

It exists to solve a narrow problem: preparing minimal, precise context
for an AI assistant (via a free web client, with no filesystem access)
without pasting an entire codebase or manually hunting down every file a
piece of code touches.

## How it works

1. Splits the given files into `.go` files and everything else, regardless
   of the order they were given in.
2. If at least one `.go` file was given, loads the whole Go module (via
   `go.mod`) with full type information, using
   [`golang.org/x/tools/go/packages`][go-packages], which is the same
   machinery `gopls` uses.
3. Starting from each `.go` file's AST, walks every identifier and
   selector expression (`x.Method()`, `pkg.Symbol`) and resolves it to the
   object it denotes via `go/types`. Multiple `.go` files are all treated
   as entry points; their closures are merged.
4. A `.go` file that doesn't show up among the module's loaded packages
   (excluded by build tags, a `_`/`.` filename, inside `testdata/`, in
   another module, etc.) is not an error: it's set aside as a **literal**
   file and included as-is, with no dependency analysis. It's labeled
   `(literal)` in the index.
5. For each resolved object, looks up the file where it was declared. If
   that file belongs to the module and hasn't been visited yet, it's
   queued for the same treatment.
6. Symbols from the standard library or external dependencies are skipped
   on purpose — an AI assistant generally already knows `fmt` or
   `net/http`, so their source isn't useful context.
7. Non-`.go` files are never analyzed; they're just included in full.
8. The final file list is written to a single output file: an index up
   top, followed by each file's full contents under a `FILE: <path>`
   header.

If no `.go` file is given at all, steps 2–6 are skipped entirely — no
module lookup, no `go.mod` required — and the result is a plain
concatenation of whatever files were given.

## Usage

```bash
# prints the concatenated context to stdout
./fileclosure path/to/your/project/pkg/handler.go

# writes it to a file instead
./fileclosure path/to/your/project/pkg/handler.go -o context.txt

# non-Go files are included as-is, alongside the computed closure
./fileclosure main.go README.md config.yaml -o context.txt

# a .go file outside the module's build is included literally, not rejected
./fileclosure main.go testdata/fixture.go -o context.txt

# multiple .go files: their closures are merged
./fileclosure pkg/handler.go pkg/worker.go -o context.txt

# no .go files at all: a plain concatenation, no analysis
./fileclosure README.md CHANGELOG.md -o context.txt

# limit how deep the dependency analysis goes: functions first reached
# beyond depth 2 are included as signatures only, not full files
./fileclosure path/to/your/project/pkg/handler.go --depth 2
```

### Arguments & Flags

* **`<files...>`** *(required, at least one)*
  `.go` files are expanded into their dependency closure
  (merged across all given); other files are included as-is. A `.go`
  file outside the module's build is included as a literal file instead
  of aborting the run.

* **`--depth`**, **`-d`** (Default: `unlimited`)
  Limit dependency depth; functions first reached beyond
  this depth are included as signatures only, not full files.

* **`--output`**, **`-o`** (Default: `stdout`)
  Output file.

Non-Go files are useful for adding context alongside the computed
closure — a `README.md`, a config file, a SQL schema. They aren't
analyzed for dependencies, just included in the order given. The same
goes for a `.go` file outside the module's build: it's included in full,
with no analysis. Both groups are marked `(literal)` in the index, with
literal `.go` files listed alphabetically ahead of the non-Go files. If a
file happens to already be part of the computed closure — or the same
file is given twice — it's only included once.

Since results go to stdout by default, `fileclosure` composes naturally
with pipes, e.g. `./fileclosure handler.go | pbcopy` or
`./fileclosure handler.go | wc -l`. Status/progress messages (warnings,
the final "OK" summary) are always written to stderr, so they never mix
into piped or redirected output.

For each `.go` file given, `fileclosure` looks for the nearest `go.mod`
above it to find the module root, then analyzes the whole module from
there. It works with any file inside a single-module project, regardless
of your current working directory. If multiple `.go` files are given,
they're expected to belong to the same module.

## Example output

```text
Input file: internal/handler/user.go
Total files included: 4

Index:
  - internal/handler/user.go
  - internal/service/user_service.go
  - internal/repository/user_repository.go
  - internal/model/user.go
======================================================================

======================================================================
FILE: internal/handler/user.go
======================================================================
package handler
...
```

With more than one `.go` file given, the header becomes plural:

```text
Input files:
  - internal/handler/user.go
  - internal/handler/order.go
Total files included: 6
...
```

## Project layout

```text
fileclosure/
├── main.go               # thin entry point, delegates to cmd.Execute()
├── cmd/
│   ├── root.go            # Cobra command: arg splitting, wiring, output
│   ├── closure.go         # unlimited closure: module loading + traversal
│   └── closure_depth.go   # depth-limited closure: signature/type extraction
└── go.mod
```

Error handling follows the standard Cobra convention: `RunE` only returns
errors, and Cobra takes care of printing them.

## Known limitations

* Only analyzes files **within the module** being analyzed; standard
  library and third-party dependency source is intentionally excluded.
  A `.go` file that falls outside the module's build is included
  literally, not analyzed.
* Doesn't follow `//go:embed` directives.
* Doesn't account for build tags / `GOOS`/`GOARCH`-specific files — it
  analyzes the default build configuration only.
* For multi-module repositories (more than one `go.mod`), run the tool
  once per module. A `.go` file belonging to another module isn't
  analyzed: it's included literally, exactly as a non-Go file would be.

[go-packages]: https://pkg.go.dev/golang.org/x/tools/go/packages
