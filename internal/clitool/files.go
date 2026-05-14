// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

// Package clitool collects the helpers that are shared between the
// luau-* command-line tools (luau, luau-compile, luau-bytecode,
// luau-ast). It mirrors the helper functions defined in upstream's
// CLI/src/FileUtils.cpp and CLI/src/Flags.cpp.
//
// luaugo's package layout differs from upstream's: upstream links the
// helpers into a Luau.CLI.lib static library that every CLI executable
// pulls in via target_link_libraries (.upstream/CMakeLists.txt:265-282).
// Here we use a Go internal package, which is the closest analogue --
// the helpers are reachable from every cmd/luau* binary but not from
// public consumers of luaugo.
package clitool

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SourceFiles walks argv (starting at index 1) and returns the list of
// source files to operate on. The rules mirror upstream's
// getSourceFiles in CLI/src/FileUtils.cpp:428:
//
//   - everything after `-a` / `--program-args` is treated as program
//     arguments and ignored.
//   - `-` is treated as a special filename for stdin.
//   - any other argument starting with `-` is skipped (it's an option,
//     not a file).
//   - bare names that point at a directory are expanded to all `.lua`
//     and `.luau` files reachable from that root (recursive).
//   - everything else is taken as a file path verbatim.
//
// Note: upstream's CLI uses `--key=value` form exclusively, so this
// argv walk never has to skip a "next-arg" option value. Go's flag
// package additionally accepts `--key value`; to keep that working we
// also accept a list of flag names that consume the next argument via
// SourceFilesSkippingFlags, which is the form used by the
// luau-bytecode and luau-compile binaries.
func SourceFiles(argv []string) []string {
	return SourceFilesSkippingFlags(argv, nil)
}

// SourceFilesSkippingFlags is SourceFiles with an explicit set of
// option names whose VALUE is consumed as the next argv element. Pass
// names without leading dashes; both `--name value` and `-name value`
// are honoured. Used by binaries that accept `--summary-file FOO`
// without the `=` separator.
func SourceFilesSkippingFlags(argv []string, takesValue map[string]bool) []string {
	files := make([]string, 0, len(argv))
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if a == "-a" || a == "--program-args" {
			return files
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			// Strip leading dashes and any `=value` to get the flag
			// name; if the bare name takes a value, the next argv
			// entry is its value, not a file.
			name := strings.TrimLeft(a, "-")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
			} else if takesValue[name] && i+1 < len(argv) {
				i++ // skip the value
			}
			continue
		}
		if a == "-" {
			files = append(files, a)
			continue
		}
		info, err := os.Stat(a)
		if err == nil && info.IsDir() {
			_ = filepath.WalkDir(a, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(path))
				if ext == ".lua" || ext == ".luau" {
					files = append(files, path)
				}
				return nil
			})
			continue
		}
		files = append(files, a)
	}
	return files
}

// ProgramArgs returns the trailing positional arguments that follow
// `-a` / `--program-args`. These are the values upstream forwards to
// the Lua chunk as varargs.
func ProgramArgs(argv []string) []string {
	for i := 1; i < len(argv); i++ {
		if argv[i] == "-a" || argv[i] == "--program-args" {
			out := make([]string, 0, len(argv)-i-1)
			out = append(out, argv[i+1:]...)
			return out
		}
	}
	return nil
}

// ReadSource reads source from a file or, when path == "-", from
// stdin. It mirrors upstream's readFile/readStdin in
// CLI/src/FileUtils.cpp, with one luaugo-specific extension:
// transparently transcode the common Windows source encodings
// (UTF-8-with-BOM, UTF-16 LE/BE) into the plain UTF-8 that the parser
// expects. Upstream parses raw bytes and so blows up with a confusing
// "Incorrect identifier" diagnostic on UTF-16 files saved by Notepad
// or `Set-Content`; that's a poor first-run experience.
//
// The transcoder only fires when a BOM is present. Files without any
// BOM are returned unmodified so we don't perturb the byte stream for
// tools that round-trip source (luau-ast --format=json, etc.).
func ReadSource(path string) ([]byte, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	return DecodeBOM(raw), nil
}

// DecodeBOM strips a UTF-8 BOM, or transcodes UTF-16 LE/BE into UTF-8,
// when the input opens with a recognized byte-order mark. Anything
// else is returned verbatim. Exposed so that callers reading source
// from somewhere other than a file (stdin, embedded resource, etc.)
// can apply the same normalisation.
func DecodeBOM(raw []byte) []byte {
	switch {
	case len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF:
		// UTF-8 BOM. Strip and keep the rest as-is.
		return raw[3:]
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE:
		// UTF-16 LE BOM. PowerShell's `Set-Content -Encoding Unicode`
		// and Notepad save in this format by default on older
		// Windows installs.
		return utf16ToUTF8(raw[2:], false)
	case len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF:
		// UTF-16 BE BOM. Less common but still surfaces from
		// hand-crafted exports.
		return utf16ToUTF8(raw[2:], true)
	}
	return raw
}

// utf16ToUTF8 decodes a UTF-16 byte slice (without BOM) to UTF-8.
// `bigEndian` selects the byte order. Lone surrogates pass through as
// U+FFFD so a malformed file doesn't take the whole CLI down.
func utf16ToUTF8(raw []byte, bigEndian bool) []byte {
	// Drop a trailing lone byte; round-trip would lose it anyway.
	n := len(raw) &^ 1
	out := make([]byte, 0, n) // UTF-8 is <= 2x UTF-16 for BMP text
	for i := 0; i < n; i += 2 {
		var u uint16
		if bigEndian {
			u = uint16(raw[i])<<8 | uint16(raw[i+1])
		} else {
			u = uint16(raw[i]) | uint16(raw[i+1])<<8
		}
		var r rune
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			// High surrogate; needs a paired low surrogate. If
			// there's no partner, or the partner is not a low
			// surrogate, emit U+FFFD and move on. `continue` to
			// skip the standard r->UTF-8 encoder below since we
			// already wrote the replacement bytes.
			if i+3 >= n {
				out = append(out, 0xEF, 0xBF, 0xBD)
				continue
			}
			var low uint16
			if bigEndian {
				low = uint16(raw[i+2])<<8 | uint16(raw[i+3])
			} else {
				low = uint16(raw[i+2]) | uint16(raw[i+3])<<8
			}
			if low < 0xDC00 || low > 0xDFFF {
				out = append(out, 0xEF, 0xBF, 0xBD)
				continue
			}
			r = ((rune(u)-0xD800)<<10 | (rune(low) - 0xDC00)) + 0x10000
			i += 2 // consume the low half too
		case u >= 0xDC00 && u <= 0xDFFF:
			// Lone low surrogate.
			out = append(out, 0xEF, 0xBF, 0xBD)
			continue
		default:
			r = rune(u)
		}
		// Encode r as UTF-8 inline (avoids pulling in unicode/utf8
		// for what is a handful of branches).
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		case r < 0x800:
			out = append(out, 0xC0|byte(r>>6), 0x80|byte(r&0x3F))
		case r < 0x10000:
			out = append(out, 0xE0|byte(r>>12), 0x80|byte((r>>6)&0x3F), 0x80|byte(r&0x3F))
		default:
			out = append(out, 0xF0|byte(r>>18), 0x80|byte((r>>12)&0x3F), 0x80|byte((r>>6)&0x3F), 0x80|byte(r&0x3F))
		}
	}
	return out
}

// ReportParseError prints a parse error in upstream's standard form
//
//	filename(line,column): SyntaxError: message
//
// to stderr. Used by every CLI to match upstream's error formatting
// (CLI/src/Compile.cpp:96-104).
func ReportParseError(name string, line, col int, msg string) {
	// Upstream reports 1-based line/column; our AST also stores
	// 1-based positions so we forward them verbatim.
	fmt.Fprintf(os.Stderr, "%s(%d,%d): SyntaxError: %s\n", name, line, col, msg)
}

// ReportCompileError prints a compile error in upstream's form
//
//	filename(line,column): CompileError: message
//
// to stderr. See CLI/src/Compile.cpp:106-109.
func ReportCompileError(name string, line, col int, msg string) {
	fmt.Fprintf(os.Stderr, "%s(%d,%d): CompileError: %s\n", name, line, col, msg)
}

// AssertionFailedHandler matches upstream's assertionHandler in
// CLI/src/Compile.cpp:445. None of the luaugo tools currently install
// a C-style assertion handler -- this stub exists so callers can
// document the intended behavior and so we can keep the upstream
// surface aligned if we add one later.
func AssertionFailedHandler(expr, file string, line int) {
	fmt.Fprintf(os.Stderr, "%s(%d): ASSERTION FAILED: %s\n", file, line, expr)
}
