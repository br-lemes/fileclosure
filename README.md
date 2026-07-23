# fileclosure

Given a Go file, `fileclosure` recursively finds every file in the same
module that it depends on — the files defining the functions, types,
variables, and constants it uses directly, the files those depend on, and
so on — and concatenates the whole closure into a single text file.

It exists to solve a narrow problem: preparing minimal, precise context
for an AI assistant (via a free web client, with no filesystem access)
without pasting an entire codebase or manually hunting down every file a
piece of code touches.

## How it works

1. Loads the whole Go module (via `go.mod`) with full type information, using
   [`golang.org/x/tools/go/packages`][go-packages], which is the same machinery
   `gopls` uses.
2. Starting from the input file's AST, walks every identifier and selector
   expression (`x.Method()`, `pkg.Symbol`) and resolves it to the object it
   denotes via `go/types`.
3. For each resolved object, looks up the file where it was declared. If
   that file belongs to the module and hasn't been visited yet, it's
   queued for the same treatment.
4. Symbols from the standard library or external dependencies are skipped
   on purpose — an AI assistant generally already knows `fmt` or
   `net/http`, so their source isn't useful context.
5. The final file list is written to a single output file: an index up
   top, followed by each file's full contents under a `FILE: <path>`
   header.

## Usage

```bash
# prints the concatenated context to stdout
./fileclosure path/to/your/project/pkg/handler.go

# writes it to a file instead
./fileclosure path/to/your/project/pkg/handler.go -o context.txt

# includes extra files as-is, without dependency analysis
./fileclosure main.go README.md config.yaml -o context.txt
```

| Argument / Flag | Shorthand | Default | Description |
| --- | --- | --- | --- |
| `<file.go>` (positional) | — | *(required)* | Entry point analyzed for the dependency closure |
| `[files...]` (positional) | — | *(none)* | Any other files (Go or not) included directly, as-is |
| `--output` | `-o` | stdout | Output file |

Extra files are useful for adding non-Go context — a `README.md`, a
config file, a SQL schema — alongside the computed closure. They aren't
analyzed for dependencies, just appended to the output in the order
given. If an extra file happens to already be part of the computed
closure, it's only included once.

Since results go to stdout by default, `fileclosure` composes naturally
with pipes, e.g. `./fileclosure handler.go | pbcopy` or
`./fileclosure handler.go | wc -l`. Status/progress messages (warnings,
the final "OK" summary) are always written to stderr, so they never mix
into piped or redirected output.

`fileclosure` looks for the nearest `go.mod` above the input file to find
the module root, then analyzes the whole module from there. It works with
any file inside a single-module project, regardless of your current
working directory.

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

## Project layout

```text
fileclosure/
├── main.go         # thin entry point, delegates to cmd.Execute()
├── cmd/
│   ├── root.go     # Cobra command: flags, wiring, output writing
│   └── closure.go  # core algorithm: module loading + AST/type traversal
└── go.mod
```

Error handling follows the standard Cobra convention: `RunE` only returns
errors, and Cobra takes care of printing them.

## Known limitations

- Only considers files **within the module** being analyzed; standard
  library and third-party dependency source is intentionally excluded.
- Doesn't follow `//go:embed` directives.
- Doesn't account for build tags / `GOOS`/`GOARCH`-specific files — it
  analyzes the default build configuration only.
- For multi-module repositories (more than one `go.mod`), run the tool
  once per module.

[go-packages]: https://pkg.go.dev/golang.org/x/tools/go/packages
