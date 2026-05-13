// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package ast_test

// conformance_parse_test.go runs the Luau parser against every .luau
// source file under tests/conformance/ and asserts the parser produces
// no errors. The task requires that at least 50/53 fixtures parse
// cleanly. Any fixtures intentionally permitted to fail should be
// listed in knownFailing below with the reason.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/luaugo/luaugo/pkg/ast"
)

// knownFailing documents fixtures that legitimately fail to parse,
// e.g. because they exercise features outside the scope of this tier.
// Each key is a fixture filename; the value is a one-line rationale.
var knownFailing = map[string]string{
	// Currently empty; populated only if a fixture is *intentionally* skipped.
}

func TestConformanceParses(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "conformance")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("cannot read conformance directory %q: %v", root, err)
	}

	var fixtures []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".luau") {
			continue
		}
		fixtures = append(fixtures, e.Name())
	}
	sort.Strings(fixtures)

	clean := 0
	var failing []string

	for _, name := range fixtures {
		path := filepath.Join(root, name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: cannot read: %v", name, err)
			continue
		}
		res := ast.Parse(name, src, ast.ParseOptions{CaptureComments: false})
		if res == nil {
			t.Errorf("%s: nil ParseResult", name)
			continue
		}
		if len(res.Errors) == 0 {
			clean++
			continue
		}
		failing = append(failing, name)
		if reason, ok := knownFailing[name]; ok {
			t.Logf("known-failing %s: %s (first err: %s @ %d:%d)",
				name, reason, res.Errors[0].Msg,
				res.Errors[0].Location.Begin.Line, res.Errors[0].Location.Begin.Column)
			continue
		}
		// Surface up to the first 3 errors so we know what's wrong.
		for i, pe := range res.Errors {
			if i >= 3 {
				break
			}
			t.Errorf("%s: %s @ %d:%d", name, pe.Msg,
				pe.Location.Begin.Line, pe.Location.Begin.Column)
		}
	}

	t.Logf("parsed cleanly: %d/%d fixtures", clean, len(fixtures))
	if clean < 50 {
		t.Fatalf("expected at least 50/53 fixtures to parse cleanly, got %d (failing: %v)", clean, failing)
	}
}
