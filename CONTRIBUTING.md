# Contributing to luaugo

Thanks for your interest. luaugo is a tightly-scoped port of upstream
Luau, so contribution norms are stricter than typical Go projects.

## Hard rules

1. **No third-party Go dependencies.** Standard library only.
2. **No cgo.** luaugo is pure Go on every supported platform.
3. **Contracts are append-only.** See `STYLE.md` &sect; *Contract files*.
4. **Bytecode byte-identity is non-negotiable.** Any change that
   alters the bytes produced by `pkg/compiler` for an existing fixture
   must be accompanied by golden regeneration and an explanation of why
   the change is necessary and compatible.

## Workflow

1. Pick an open issue or coordinate with the orchestrator.
2. Implement against existing `contract.go` signatures only.
3. Run `go build ./... && go vet ./... && go test ./...`.
4. Submit a patch.

See `STYLE.md` for license headers, naming, GC discipline, and other
project conventions.
