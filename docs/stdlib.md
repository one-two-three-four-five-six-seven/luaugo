# Luau standard library (as exposed by luaugo)

This is the user-facing reference for every standard-library function
that scripts running on the luaugo VM can call. Behavior matches
upstream Luau unless explicitly noted; for the full semantics see the
[official Luau library reference](https://luau.org/library).

To make these functions available to a script, open the relevant
libraries on the VM state:

```go
s := vm.NewState()
lib.OpenAll(s)        // open everything
// or selectively:
lib.OpenBase(s)
lib.OpenMath(s)
lib.OpenString(s)
// ...
```

## base (always-open globals)

Function | Signature | Notes
--- | --- | ---
`assert` | `assert(v[, msg])` | Errors with `msg` (default "assertion failed!") if `v` is falsy. Returns its arguments.
`collectgarbage` | `collectgarbage([opt])` | Only `"count"` is meaningful (returns kilobytes from `gcinfo`). Other options are no-ops.
`error` | `error(msg[, level])` | Raises a runtime error. With `level > 0`, prepends source location when `msg` is a string.
`gcinfo` | `gcinfo()` | Total Lua heap size in kilobytes.
`getfenv` | `getfenv([f|level])` | Returns the calling function's environment. luaugo returns the `_G` proxy.
`getmetatable` | `getmetatable(v)` | Honors `__metatable` lock.
`ipairs` | `ipairs(t)` | Standard array iterator (stops at first nil).
`loadstring` | `loadstring(s)` | Returns `nil, "loadstring disabled"`. luaugo deliberately omits a runtime parser; compile separately.
`newproxy` | `newproxy([mt])` | Creates a userdata; with `true`, attaches an empty metatable.
`next` | `next(t[, k])` | Table-traversal primitive.
`pairs` | `pairs(t)` | Generic table iterator.
`pcall` | `pcall(f, ...)` | Returns `(true, results...)` on success, `(false, msg)` on error.
`print` | `print(...)` | Writes tab-separated `tostring`'d args plus newline. Output target is `lib.Stdout` (an `io.Writer`, defaults to `os.Stdout`; can be redirected).
`rawequal` | `rawequal(a, b)` | Raw equality bypassing `__eq`.
`rawget` | `rawget(t, k)` | Bypasses `__index`.
`rawlen` | `rawlen(s | t)` | Bypasses `__len`.
`rawset` | `rawset(t, k, v)` | Bypasses `__newindex`. Returns `t`.
`select` | `select(i | '#', ...)` | With `'#'` returns arg count; with `i` returns the i-th onward.
`setfenv` | `setfenv(level|f, env)` | Best-effort; rarely useful.
`setmetatable` | `setmetatable(t, mt)` | Errors if `t` has a protected `__metatable`.
`tonumber` | `tonumber(s[, base])` | Parses numeric literals including hex `0x`, binary `0b`, and underscore separators.
`tostring` | `tostring(v)` | Invokes `__tostring` if present.
`type` | `type(v)` | Returns the lowercase type name.
`typeof` | `typeof(v)` | Like `type` but consults a userdata's `__type` field if set.
`unpack` | `unpack(t[, i[, j]])` | Same as `table.unpack`.
`xpcall` | `xpcall(f, handler, ...)` | Like `pcall` but routes errors through `handler`.
`_G` | global table proxy | Reads/writes forward to the real globals.
`_VERSION` | `"Luau"` | Identifier string.

## math

All 37 functions plus the constants `math.pi`, `math.huge`, `math.nan`,
`math.e`, `math.phi`, `math.sqrt2`, `math.tau`.

- **Trig**: `sin`, `cos`, `tan`, `asin`, `acos`, `atan`, `atan2`, `sinh`,
  `cosh`, `tanh`, `deg`, `rad`. All angles in radians.
- **Power / log**: `sqrt`, `pow`, `exp`, `log` (with optional base),
  `log10`, `ldexp`, `frexp`.
- **Rounding**: `floor`, `ceil`, `round` (half away from zero), `modf`,
  `fmod`, `sign`.
- **Comparison**: `min`, `max`, `clamp`.
- **Interpolation**: `lerp(a, b, t)` (returns `b` exactly when `t == 1`),
  `map(x, in_min, in_max, out_min, out_max)`.
- **Random**: `random()` returns `[0, 1)`, `random(m)` returns `[1, m]`,
  `random(m, n)` returns `[m, n]`. `randomseed(seed)` seeds
  deterministically.
- **Predicates**: `isnan`, `isinf`, `isfinite`.
- **Noise**: `noise(x[, y[, z]])` &mdash; full 3D Perlin noise, port of
  upstream's gradient and permutation tables.
- **Misc**: `abs`.

## string

Full Luau string library, including the complete Lua pattern matcher.

Function | Notes
--- | ---
`string.byte(s[, i[, j]])` | Returns byte codes.
`string.char(...)` | Builds a string from byte codes.
`string.find(s, pat[, init[, plain]])` | Returns `start, end[, captures...]` or `nil`.
`string.format(fmt, ...)` | Supports `c d i u o x X e E f g G q s` plus `%*` and `%%`. Flags `- + # 0 ` (space), 2-digit width and precision.
`string.gmatch(s, pat)` | Iterator producing captures (or whole match if no captures).
`string.gsub(s, pat, repl[, n])` | `repl` may be string (with `%N` substitutions), number, table, or function.
`string.len(s)` | Byte length.
`string.lower(s)` | ASCII lowercase.
`string.match(s, pat[, init])` | Returns captures (or whole match).
`string.pack(fmt, ...)` | Lua 5.3 pack format; fixed sizes (short=16, long=64, int=32, size_t=32).
`string.packsize(fmt)` | Size in bytes the format will produce. Errors on variable-length specifiers.
`string.rep(s, n)` | Repeats `s`.
`string.reverse(s)` | Byte-reverse.
`string.split(s[, sep])` | Luau extension. Default sep is `","`. Empty sep splits into one-byte pieces.
`string.sub(s, i[, j])` | Substring with negative-index support.
`string.unpack(fmt, s)` | Inverse of `pack`.
`string.upper(s)` | ASCII uppercase.

Pattern syntax covered (100%):

- Classes `%a %A %c %C %d %D %g %G %l %L %p %P %s %S %u %U %w %W %x %X %z`.
- Sets `[abc]`, ranges `[a-z]`, negation `[^...]`, escapes inside sets.
- Anchors `^` and `$`.
- Captures `(...)`, position captures `()`, balanced `%bxy`, frontier `%f[set]`.
- Back-references `%1` ... `%9` in patterns and `gsub` replacements.
- Quantifiers: greedy `*`, `+`, `?`, lazy `-`.

String methods (`("hello"):upper()`) work because OpenString wires a
metatable for the string type pointing back at the library table.

## table

Function | Notes
--- | ---
`table.clear(t)` | Removes every key while preserving capacity.
`table.clone(t)` | Shallow copy (preserves metatable; result is not frozen).
`table.concat(t[, sep[, i[, j]]])` | Concatenates string-valued entries.
`table.create(n[, v])` | Preallocated array of `n` slots, optionally filled with `v`.
`table.find(t, v[, init])` | Linear search; returns index or `nil`.
`table.foreach`, `table.foreachi`, `table.getn` | Deprecated but present.
`table.freeze(t)` | Marks `t` read-only. Subsequent writes raise.
`table.insert(t, v)` / `table.insert(t, i, v)` | Append or shift-insert.
`table.isfrozen(t)` | Reports the frozen flag.
`table.maxn(t)` | Maximum positive numeric key.
`table.move(a, f, e, d[, dst])` | Overlap-safe range copy.
`table.pack(...)` | Returns `{n=count, ...}`.
`table.remove(t[, i])` | Shift-remove; default at end.
`table.sort(t[, cmp])` | Stable introsort using `cmp` (default `<`).
`table.unpack(t[, i[, j]])` | Inverse of `pack`.

## coroutine

All 8 functions. Implementation uses one Go goroutine per coroutine plus
a per-VM scheduler mutex; race-detector clean.

Function | Notes
--- | ---
`coroutine.close(co)` | Closes a dead or suspended coroutine.
`coroutine.create(f)` | Wraps `f` in a thread; thread is initially suspended.
`coroutine.isyieldable()` | True when called from inside a coroutine that can currently yield.
`coroutine.resume(co, ...)` | Returns `(true, values...)` on success or `(false, err)` on error.
`coroutine.running()` | The currently running thread, or `nil` for the main thread.
`coroutine.status(co)` | `"running" / "suspended" / "normal" / "dead"`.
`coroutine.wrap(f)` | Like `create` + `resume`, returns a callable.
`coroutine.yield(...)` | Suspends current coroutine; values flow to the resumer.

## bit32

All 15 functions, operating on `uint32` representations.

Function | Notes
--- | ---
`bit32.arshift(n, i)` | Arithmetic right shift (sign-preserving).
`bit32.band(...)` | Default `~0`.
`bit32.bnot(n)` | Bitwise NOT.
`bit32.bor(...)` | Default `0`.
`bit32.btest(...)` | True iff `band(...) != 0`.
`bit32.bxor(...)` | Default `0`.
`bit32.byteswap(n)` | Reverses byte order.
`bit32.countlz(n)` | Leading-zero count (32 if `n == 0`).
`bit32.countrz(n)` | Trailing-zero count.
`bit32.extract(n, f[, w])` | Bits `[f, f+w-1]` as unsigned. `w` defaults to 1.
`bit32.lrotate(n, i)` | Left rotate.
`bit32.lshift(n, i)` | Logical left shift; `|i| >= 32` gives `0`.
`bit32.replace(n, v, f[, w])` | Inserts `v` into bits `[f, f+w-1]`.
`bit32.rrotate(n, i)` | Right rotate.
`bit32.rshift(n, i)` | Logical right shift.

## utf8

Function | Notes
--- | ---
`utf8.char(...)` | Build a string from one or more codepoints.
`utf8.charpattern` | Pattern matching one UTF-8 codepoint (`"[\0-\x7F\xC2-\xF4][\x80-\xBF]*"`).
`utf8.codepoint(s[, i[, j]])` | Returns codepoints between byte offsets.
`utf8.codes(s)` | Iterator: `(byte_offset, codepoint)` per codepoint.
`utf8.len(s[, i[, j]])` | Counts codepoints. Returns `(nil, badbyte)` on malformed UTF-8.
`utf8.offset(s, n[, i])` | Byte offset of the n-th codepoint starting from byte `i`. Negative `n` counts backward.

Decoder is strict: overlong encodings, surrogates, and codepoints
beyond `0x10FFFF` are rejected.

## os

Function | Notes
--- | ---
`os.clock()` | High-resolution seconds since process start.
`os.date([fmt[, t]])` | `fmt` defaults to `"%c"`; prefix with `!` for UTC. `"*t"` / `"!*t"` returns a table.
`os.difftime(a, b)` | Computes `a - b`.
`os.time([t])` | No-arg: Unix timestamp. With a `{year=..., month=..., day=..., hour=..., min=..., sec=...}` table: composes a UTC timestamp.

`date` accepts a wide subset of strftime: `%a %A %b %B %c %d %H %I %j %m %M %p %S %U %w %W %x %X %y %Y %z %Z %%`.

## debug

Function | Notes
--- | ---
`debug.info([co,] level | f, what)` | `what` is a string of option letters. `s` = source, `l` = line, `n` = name, `f` = function value, `a` = numargs + isvararg.
`debug.traceback([co,] [msg,] [level])` | Returns a multi-line stack trace string. With `msg` prepended.

Lines reported via `debug.info(..., "l")` and inside tracebacks are
currently 0 for luaugo-compiled functions while the compiler does not
yet emit line info. Tracebacks against upstream-compiled bytecode show
accurate lines.

## buffer

Fixed-size mutable byte array. Functions all raise on out-of-bounds.

Function | Notes
--- | ---
`buffer.copy(dst, dstOff, src[, srcOff[, count]])` | Overlap-safe.
`buffer.create(size)` | New buffer of given size, zeroed. Max 1 GiB.
`buffer.fill(b, off, val[, count])` | Fills bytes with `val & 0xff`.
`buffer.fromstring(s)` | New buffer initialized to string contents.
`buffer.len(b)` | Size in bytes.
`buffer.readbits(b, bitOff, bitCount)` | `bitCount` in `[0, 32]`.
`buffer.writebits(b, bitOff, bitCount, val)` | As above.
`buffer.readi8/u8/i16/u16/i32/u32(b, off)` | Signed and unsigned reads (LE).
`buffer.readf32/f64(b, off)` | IEEE-754 LE reads.
`buffer.readstring(b, off, count)` | Reads `count` bytes into a string.
`buffer.tostring(b)` | Returns the whole buffer as a string.
`buffer.writei8/u8/i16/u16/i32/u32(b, off, val)` | Truncating integer writes (LE).
`buffer.writef32/f64(b, off, val)` | IEEE-754 LE writes.
`buffer.writestring(b, off, val[, count])` | Writes up to `count` bytes of `val`.

All multi-byte accesses are little-endian.

## vector

3-wide vectors (`x`, `y`, `z`); component access via `v.x`, `v.y`,
`v.z` (and `v.X`/etc.).

Function | Notes
--- | ---
`vector.abs(v)` | Per-component absolute value.
`vector.angle(a, b[, axis])` | Angle in radians. `axis` flips sign by `(a x b) . axis`.
`vector.ceil(v)` | Per-component ceil.
`vector.clamp(v, min, max)` | Per-component clamp.
`vector.create(x, y, z)` | Constructor.
`vector.cross(a, b)` | 3D cross product.
`vector.dot(a, b)` | Dot product.
`vector.floor(v)` | Per-component floor.
`vector.lerp(a, b, t)` | Per-component; returns `b` exactly when `t == 1`.
`vector.magnitude(v)` | `sqrt(x*x + y*y + z*z)`.
`vector.max(...)` | Per-component max across all input vectors.
`vector.min(...)` | Per-component min.
`vector.normalize(v)` | Unit vector. Returns `(0,0,0)` for the zero input.
`vector.sign(v)` | Per-component sign.
`vector.one` | Constant `(1, 1, 1)`.
`vector.zero` | Constant `(0, 0, 0)`.
