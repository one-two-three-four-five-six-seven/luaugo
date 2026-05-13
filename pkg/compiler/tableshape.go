// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

import "github.com/luaugo/luaugo/pkg/ast"

// tableshape.go ports the table-shape detection from
// Compiler/src/TableShape.cpp.
//
// A "shaped" table is a constructor whose keys are all known at compile
// time (all string-literal record entries). Upstream emits DUPTABLE for
// those, copying a pre-populated template at runtime. We perform the
// same detection in compileTable; this file collects the helpers.

// isStringRecordShape reports whether every entry in t is a record
// `key = value` with a literal string key. The compileTable path uses
// this signal to choose between NEWTABLE+SETTABLEKS and DUPTABLE.
func isStringRecordShape(t *ast.ExprTable) bool {
	if len(t.Items) == 0 {
		return false
	}
	for _, it := range t.Items {
		if it.Kind != ast.TableItemRecord {
			return false
		}
		if _, ok := it.Key.(*ast.ExprConstantString); !ok {
			return false
		}
	}
	return true
}
