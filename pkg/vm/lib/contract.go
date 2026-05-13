// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

// Package lib provides Luau's standard libraries. Each Lua library has
// a dedicated Go file (base.go, math.go, ...). Every Open* function
// registers its library's globals or library table on the supplied
// state. Calling OpenAll opens the same set that upstream Luau opens
// from luaL_openlibs / linit.cpp.
package lib

import "github.com/luaugo/luaugo/pkg/vm"

// OpenAll opens every standard library on s. It is the equivalent of
// upstream luaL_openlibs / linit.cpp's set of openlib calls.
func OpenAll(s *vm.State) {
	OpenBase(s)
	OpenCoroutine(s)
	OpenTable(s)
	OpenOS(s)
	OpenString(s)
	OpenBit32(s)
	OpenBuffer(s)
	OpenUTF8(s)
	OpenMath(s)
	OpenDebug(s)
	OpenVector(s)
}

// Per-library entry points. Each is implemented in its own file.

// OpenBase registers the `_G` globals (print, type, pairs, ...).
func OpenBase(s *vm.State) { openBase(s) }

// OpenMath registers the math library.
func OpenMath(s *vm.State) { openMath(s) }

// OpenString registers the string library and the metatable for the
// string type.
func OpenString(s *vm.State) { openString(s) }

// OpenTable registers the table library.
func OpenTable(s *vm.State) { openTable(s) }

// OpenCoroutine registers the coroutine library.
func OpenCoroutine(s *vm.State) { openCoroutine(s) }

// OpenBit32 registers the bit32 library.
func OpenBit32(s *vm.State) { openBit32(s) }

// OpenUTF8 registers the utf8 library.
func OpenUTF8(s *vm.State) { openUTF8(s) }

// OpenOS registers the os library.
func OpenOS(s *vm.State) { openOS(s) }

// OpenDebug registers the debug library.
func OpenDebug(s *vm.State) { openDebug(s) }

// OpenBuffer registers the buffer library.
func OpenBuffer(s *vm.State) { openBuffer(s) }

// OpenVector registers the vector library.
func OpenVector(s *vm.State) { openVector(s) }
