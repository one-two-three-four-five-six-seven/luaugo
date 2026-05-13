// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"reflect"
	"unsafe"
)

// uintptrOf returns a uintptr that is stable for the lifetime of v
// (assuming v carries a pointer). The collector pins all Lua-managed
// objects via the allgc list, so no relocation happens behind our back
// and this value is usable as a hash seed.
//
// The reflect path handles non-pointer GC objects (chiefly the
// gcObject interface header) by extracting the underlying data word.
func uintptrOf(v any) uintptr {
	if v == nil {
		return 0
	}
	switch p := v.(type) {
	case unsafe.Pointer:
		return uintptr(p)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.UnsafePointer, reflect.Chan, reflect.Map, reflect.Func:
		return rv.Pointer()
	case reflect.Interface:
		return uintptrOf(rv.Elem().Interface())
	}
	// Fallback: hash the address of the boxed copy. This is stable
	// within a single boxing of v but not across boxings.
	return uintptr(unsafe.Pointer(&v))
}
