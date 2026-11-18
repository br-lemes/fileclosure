package cmd

import (
	"os"
	"regexp"
	"strings"
)

func templSource(goFile string) (string, bool) {
	if !strings.HasSuffix(goFile, "_templ.go") {
		return "", false
	}
	src := strings.TrimSuffix(goFile, "_templ.go") + ".templ"
	if _, err := os.Stat(src); err != nil {
		return "", false
	}
	return src, true
}

func generatedFor(templFile string) string {
	return strings.TrimSuffix(templFile, ".templ") + "_templ.go"
}

// displayPath is for output only: the .templ source is what matters,
// the generated Go is boilerplate.
func displayPath(path string) string {
	if src, ok := templSource(path); ok {
		return src
	}
	return path
}

var templDeclRe = regexp.MustCompile(`(?m)^[ \t]*templ[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)

// templSignature returns a component's declaration header without its
// body, because depth-limited mode shows components reached beyond the
// limit as signatures only.
func templSignature(src, name string) (string, bool) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", false
	}
	s := string(data)
	for _, loc := range templDeclRe.FindAllStringSubmatchIndex(s, -1) {
		if s[loc[2]:loc[3]] != name {
			continue
		}
		i := skipSpace(s, loc[1])
		if i < len(s) && s[i] == '[' {
			var ok bool
			i, ok = skipBalanced(s, i, '[', ']')
			if !ok {
				return "", false
			}
			i = skipSpace(s, i)
		}
		if i >= len(s) || s[i] != '(' {
			return "", false
		}
		end, ok := skipBalanced(s, i, '(', ')')
		if !ok {
			return "", false
		}
		return strings.TrimSpace(s[loc[0]:end]), true
	}
	return "", false
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' ||
		s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

func skipBalanced(s string, start int, open, close byte) (int, bool) {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}
