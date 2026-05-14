// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import "github.com/luaugo/luaugo/pkg/vm"

// debug.go implements Luau's debug library. Mirrors upstream
// .upstream/VM/src/ldblib.cpp. Luau's debug library is deliberately
// minimal: it exposes only `debug.info` and `debug.traceback`.

// openDebug registers the `debug` library table as a global. Mirrors
// upstream luaopen_debug.
func openDebug(s *vm.State) {
	s.CreateTable(0, 2)
	s.LRegisterList([]vm.LFnEntry{
		{Name: "info", Fn: debugInfo},
		{Name: "traceback", Fn: debugTraceback},
	})
	s.PushValue(-1)
	s.SetGlobal("debug")
	s.Pop(1)
}

// getDebugThread mirrors upstream getthread(L, &arg): if the first arg
// is a thread, return that thread and shift the argument index by one.
// Otherwise return s itself with arg == 0.
//
// Returned `arg` is the upstream `arg` value (0 if no thread arg was
// consumed, 1 if it was). luaugo's argument indices are 1-based, so
// the next argument's index is arg + 1.
func getDebugThread(s *vm.State) (target *vm.State, arg int) {
	if s.Type(1) == vm.TThread {
		t := s.ToThread(1)
		if t == nil {
			s.LArgError(1, "thread expected")
		}
		return t, 1
	}
	return s, 0
}

// debugInfo implements debug.info. Accepted signatures:
//
//	debug.info(thread, level, what)
//	debug.info(level, what)
//	debug.info(function, what)
//
// `what` is a string of single-letter codes:
//
//	s : source path (chunkname)
//	l : current source line
//	n : function name ("" if unknown)
//	f : the function value itself
//	a : nparams (integer) followed by isvararg (boolean)
//
// Results are pushed in the order of the option letters. Duplicate
// letters raise an argument error, matching upstream.
func debugInfo(s *vm.State) int {
	target, arg := getDebugThread(s)

	// Determine level / function position.
	var level int
	var fnIdx int // stack index of the function being inspected, 0 if none

	switch {
	case s.IsNumber(arg + 1):
		n64, _ := s.ToInteger(arg + 1)
		if n64 < 0 {
			s.LArgError(arg+1, "level can't be negative")
		}
		level = int(n64)
	case arg == 0 && s.IsFunction(1):
		fnIdx = 1
	default:
		s.LArgError(arg+1, "function or level expected")
	}

	options := s.LCheckString(arg + 2)

	// Look up frame info. The function-as-arg form has no frame; we
	// produce a minimal info record because luaugo's Tier-3 API does
	// not yet expose closure introspection of an arbitrary function
	// value.
	var info vm.DebugInfo
	if fnIdx == 0 {
		var ok bool
		info, ok = target.GetInfo(level)
		if !ok {
			return 0
		}
	}

	var occurs [26]bool
	results := 0

	for i := 0; i < len(options); i++ {
		c := options[i]
		if c >= 'a' && c <= 'z' {
			if occurs[c-'a'] {
				s.LArgError(arg+2, "duplicate option")
			}
			occurs[c-'a'] = true
		}

		switch c {
		case 's':
			if fnIdx != 0 {
				s.PushString("[Lua]")
			} else {
				s.PushString(info.Source)
			}
			results++

		case 'l':
			if fnIdx != 0 {
				s.PushInteger(-1)
			} else {
				s.PushInteger(int64(info.Currentline))
			}
			results++

		case 'n':
			if fnIdx != 0 {
				s.PushString("")
			} else {
				s.PushString(info.Name)
			}
			results++

		case 'f':
			if fnIdx != 0 {
				s.PushValue(fnIdx)
			} else if target == s {
				if !s.PushFunc(level) {
					s.PushNil()
				}
			} else {
				// Cross-thread function transfer not yet supported via
				// the Tier-3 surface; push nil rather than crashing.
				s.PushNil()
			}
			results++

		case 'a':
			if fnIdx != 0 {
				s.PushInteger(0)
				s.PushBoolean(false)
			} else {
				s.PushInteger(int64(info.NumParams))
				s.PushBoolean(info.IsVararg)
			}
			results += 2

		default:
			s.LArgError(arg+2, "invalid option")
		}
	}

	return results
}

// debugTraceback implements debug.traceback(co?, msg?, level?).
// Returns the traceback string. If msg is present but not a string,
// it is returned verbatim (matching upstream luaL_traceback, which
// also degrades to "return the message unchanged" for non-string
// values).
func debugTraceback(s *vm.State) int {
	target, arg := getDebugThread(s)

	var msg string
	if !s.IsNoneOrNil(arg + 1) {
		if s.IsString(arg + 1) {
			msg = s.LCheckString(arg + 1)
		} else {
			// Non-string msg: return it as the single result.
			s.PushValue(arg + 1)
			return 1
		}
	}

	defaultLevel := int64(1)
	if target != s {
		defaultLevel = 0
	}
	level64 := s.LOptInteger(arg+2, defaultLevel)
	if level64 < 0 {
		s.LArgError(arg+2, "level can't be negative")
	}

	out := target.TraceBack(int(level64), msg)
	s.PushString(out)
	return 1
}
