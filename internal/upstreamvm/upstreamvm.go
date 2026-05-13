// Copyright (c) luaugo contributors. Licensed under the MIT License.

// Package upstreamvm is a tiny test-only helper that drives the
// upstream Luau VM via the tools/luau-bcrunner harness.
//
// This package is the contract surface tests use to verify that
// luaugo-produced bytecode is semantically valid Luau bytecode that
// loads and executes correctly against the official upstream VM.
//
// Build the harness once with: tools/luau-bcrunner/build.ps1
// The resulting binary is gitignored; tests skip gracefully when it
// is absent.
package upstreamvm

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Status enumerates the four possible outcomes of running a bytecode
// blob through the upstream VM.
type Status int

const (
	// StatusOK indicates the chunk loaded and ran to completion.
	StatusOK Status = 0
	// StatusIO indicates the harness could not read the blob.
	StatusIO Status = 1
	// StatusLoadError indicates luau_load rejected the bytecode.
	StatusLoadError Status = 2
	// StatusRuntimeError indicates lua_pcall caught a runtime error
	// (which still implies the bytecode loaded successfully).
	StatusRuntimeError Status = 3
)

// Result is what Run returns.
type Result struct {
	Status   Status
	Stdout   string // text written to stdout by the chunk
	Stderr   string // text written to stderr by the harness or VM
	ExitCode int
}

// LoadOK reports whether the bytecode at least successfully loaded.
// Runtime errors after load still count as "load OK" because the
// bytecode itself was valid.
func (r Result) LoadOK() bool {
	return r.Status == StatusOK || r.Status == StatusRuntimeError
}

var (
	harnessOnce sync.Once
	harnessPath string
	harnessErr  error
)

// HarnessPath returns the absolute path to bcrunner.exe (Windows) or
// bcrunner (other platforms), or an error if it cannot be located.
func HarnessPath() (string, error) {
	harnessOnce.Do(func() {
		harnessPath, harnessErr = locate()
	})
	return harnessPath, harnessErr
}

func locate() (string, error) {
	// Walk up from the caller's working dir looking for go.mod, then
	// resolve tools/luau-bcrunner/bcrunner[.exe] from there.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("upstreamvm: could not locate go.mod above cwd")
		}
		dir = parent
	}
	name := "bcrunner"
	if runtime.GOOS == "windows" {
		name = "bcrunner.exe"
	}
	bin := filepath.Join(dir, "tools", "luau-bcrunner", name)
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("upstreamvm: harness not built at %s; run tools/luau-bcrunner/build.ps1", bin)
	}
	return bin, nil
}

// Available reports whether the harness binary is present.
func Available() bool {
	_, err := HarnessPath()
	return err == nil
}

// RequireAvailable skips the test if the harness is not available.
func RequireAvailable(t testing.TB) {
	t.Helper()
	if !Available() {
		_, err := HarnessPath()
		t.Skipf("upstream VM harness unavailable: %v", err)
	}
}

// RunOptions controls how Run invokes the bcrunner harness.
type RunOptions struct {
	// NoSandbox disables luaL_sandbox/luaL_sandboxthread, allowing
	// the script to declare new globals.
	NoSandbox bool
	// NoPrintOverride uses upstream's default print() instead of
	// bcrunner's tab-delimited variant.
	NoPrintOverride bool
	// Chunkname overrides the bytecode chunk identity passed to
	// luau_load. Use "=NAME" to get bare "NAME:line:" in error
	// messages instead of bcrunner's default `[string "PATH"]:line:`.
	Chunkname string
}

// Run executes blob on the upstream Luau VM and returns the result.
// blob must be valid Luau bytecode (e.g. produced by bytecode.Encode
// or by upstream luau-compile --binary).
func Run(blob []byte) (Result, error) { return RunWith(blob, RunOptions{}) }

// RunWith is Run with explicit harness options.
func RunWith(blob []byte, opts RunOptions) (Result, error) {
	bin, err := HarnessPath()
	if err != nil {
		return Result{}, err
	}
	tmp, err := os.CreateTemp("", "luaugo-bcrunner-*.luac")
	if err != nil {
		return Result{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return Result{}, err
	}
	if err := tmp.Close(); err != nil {
		return Result{}, err
	}

	args := []string{}
	if opts.NoSandbox {
		args = append(args, "--no-sandbox")
	}
	if opts.NoPrintOverride {
		args = append(args, "--no-print-override")
	}
	if opts.Chunkname != "" {
		args = append(args, "--chunkname", opts.Chunkname)
	}
	args = append(args, tmpName)

	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	result := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if ee, ok := runErr.(*exec.ExitError); ok {
		result.ExitCode = ee.ExitCode()
		result.Status = Status(result.ExitCode)
	} else if runErr != nil {
		return result, runErr
	} else {
		result.Status = StatusOK
	}
	return result, nil
}

// MustRun is like Run but fails the test on harness invocation error.
func MustRun(t testing.TB, blob []byte) Result {
	t.Helper()
	r, err := Run(blob)
	if err != nil {
		t.Fatalf("upstreamvm.Run: %v", err)
	}
	return r
}

// RunSource compiles source via upstream luau-compile (which must be
// on PATH or in C:\Users\user\Downloads\luau-windows\) and returns the
// upstream VM's execution result. Used to produce a reference output
// to compare luaugo's against.
func RunSource(source []byte) (Result, error) {
	return RunSourceWith(source, RunOptions{})
}

// RunSourceWith is RunSource with explicit harness options.
func RunSourceWith(source []byte, opts RunOptions) (Result, error) {
	blob, err := CompileSource(source)
	if err != nil {
		return Result{}, err
	}
	return RunWith(blob, opts)
}

// CompileSource invokes upstream luau-compile --binary on source and
// returns the resulting bytecode blob.
func CompileSource(source []byte) ([]byte, error) {
	luauCompile, err := findLuauCompile()
	if err != nil {
		return nil, err
	}
	tmpSrc, err := os.CreateTemp("", "luaugo-src-*.luau")
	if err != nil {
		return nil, err
	}
	tmpSrcName := tmpSrc.Name()
	defer os.Remove(tmpSrcName)
	if _, err := tmpSrc.Write(source); err != nil {
		tmpSrc.Close()
		return nil, err
	}
	tmpSrc.Close()

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(luauCompile, "--binary", tmpSrcName)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("luau-compile failed: %w; stderr=%s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func findLuauCompile() (string, error) {
	if p, err := exec.LookPath("luau-compile"); err == nil {
		return p, nil
	}
	// Workspace fallback location used in this development environment.
	candidates := []string{
		`C:\Users\user\Downloads\luau-windows\luau-compile.exe`,
		`C:\Users\user\Documents\luaugo\.upstream-bin\luau-compile.exe`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("upstreamvm: luau-compile not found on PATH or in workspace fallbacks")
}

// NormalizeStdout strips a trailing newline. Useful when comparing two
// runs that may differ only in line-ending presence.
func NormalizeStdout(s string) string { return strings.TrimRight(s, "\r\n") }
