// Copyright (c) luaugo contributors. Licensed under the MIT License.
// luau-bcrunner: feed a precompiled Luau bytecode blob to the official
// Luau VM and observe the result. Reads bytecode from a file path
// supplied as a positional argument, or stdin if the path is "-".
//
// Usage: bcrunner [--no-sandbox] [--no-print-override]
//                 [--chunkname NAME] <bytecode-file|->
//
// Flags:
//   --no-sandbox         Skip luaL_sandbox/luaL_sandboxthread. Allows
//                        the script to declare new globals (required
//                        for the upstream conformance fixtures which
//                        define top-level helper functions).
//   --no-print-override  Use upstream's default print() instead of the
//                        in-harness writeStdout (which discards
//                        trailing newlines on empty calls).
//   --chunkname NAME     Use NAME as the chunk identity passed to
//                        luau_load (default: the file path). Some
//                        fixtures embed their expected source file
//                        name in error-message assertions.
//
// Exit codes:
//   0  - bytecode loaded and main chunk returned (results printed to stdout)
//   1  - I/O error reading the bytecode blob
//   2  - luau_load rejected the bytecode (load-time error printed to stderr)
//   3  - lua_pcall returned a runtime error (error printed to stderr)
//
// Output protocol on success: one line per return value, formatted via
// lua_tolstring (after first calling __tostring on each).

#include "lua.h"
#include "lualib.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

static std::vector<char> readAll(FILE* f) {
    std::vector<char> buf;
    char chunk[8192];
    size_t n;
    while ((n = fread(chunk, 1, sizeof chunk, f)) > 0) {
        buf.insert(buf.end(), chunk, chunk + n);
    }
    return buf;
}

static int writeStdout(lua_State* L) {
    int n = lua_gettop(L);
    for (int i = 1; i <= n; ++i) {
        size_t l = 0;
        const char* s = luaL_tolstring(L, i, &l);
        if (i > 1) fputc('\t', stdout);
        fwrite(s, 1, l, stdout);
        lua_pop(L, 1); // luaL_tolstring pushes a copy
    }
    fputc('\n', stdout);
    return 0;
}

int main(int argc, char** argv) {
    bool sandbox = true;
    bool overridePrint = true;
    const char* path = nullptr;
    const char* chunkname = nullptr;

    for (int i = 1; i < argc; ++i) {
        if (strcmp(argv[i], "--no-sandbox") == 0) {
            sandbox = false;
        } else if (strcmp(argv[i], "--no-print-override") == 0) {
            overridePrint = false;
        } else if (strcmp(argv[i], "--chunkname") == 0) {
            if (i + 1 >= argc) {
                fprintf(stderr, "bcrunner: --chunkname requires a value\n");
                return 1;
            }
            chunkname = argv[++i];
        } else if (argv[i][0] == '-' && argv[i][1] != '\0') {
            fprintf(stderr, "bcrunner: unknown flag %s\n", argv[i]);
            return 1;
        } else {
            path = argv[i];
        }
    }
    if (!path) {
        fprintf(stderr,
            "usage: %s [--no-sandbox] [--no-print-override] "
            "[--chunkname NAME] <bytecode-file|->\n",
            argv[0]);
        return 1;
    }
    if (!chunkname) chunkname = path;

    std::vector<char> blob;
    if (strcmp(path, "-") == 0) {
        blob = readAll(stdin);
    } else {
        FILE* f = fopen(path, "rb");
        if (!f) {
            fprintf(stderr, "bcrunner: cannot open %s\n", path);
            return 1;
        }
        blob = readAll(f);
        fclose(f);
    }

    if (blob.empty()) {
        fprintf(stderr, "bcrunner: empty bytecode blob\n");
        return 1;
    }

    lua_State* L = luaL_newstate();
    luaL_openlibs(L);

    if (overridePrint) {
        lua_pushcfunction(L, writeStdout, "print");
        lua_setglobal(L, "print");
    }

    if (sandbox) {
        luaL_sandbox(L);
        luaL_sandboxthread(L);
    }

    int loaded = luau_load(L, chunkname, blob.data(), blob.size(), 0);
    if (loaded != 0) {
        const char* msg = lua_tostring(L, -1);
        fprintf(stderr, "bcrunner: load error: %s\n", msg ? msg : "(no message)");
        lua_close(L);
        return 2;
    }

    int rc = lua_pcall(L, 0, LUA_MULTRET, 0);
    if (rc != LUA_OK) {
        const char* msg = lua_tostring(L, -1);
        fprintf(stderr, "bcrunner: runtime error: %s\n", msg ? msg : "(no message)");
        lua_close(L);
        return 3;
    }

    // Print main-chunk return values, if any.
    int n = lua_gettop(L);
    for (int i = 1; i <= n; ++i) {
        size_t l = 0;
        const char* s = luaL_tolstring(L, i, &l);
        if (i > 1) fputc('\t', stdout);
        fwrite(s, 1, l, stdout);
        lua_pop(L, 1);
    }
    if (n > 0) fputc('\n', stdout);

    lua_close(L);
    return 0;
}
