# Build Instructions

## Prerequisites
- **Build Tool**: Go 1.25 (module `github.com/mwieczorkiewicz/opcua-mcp`, `go.mod`'s `go 1.25.0` directive)
- **Dependencies**: resolved via `go.mod`/`go.sum` - no separate install step; `go build`/`go test` fetch/verify automatically from the module cache
- **Environment Variables**: none required to build; `OPCUA_ENDPOINT` etc. are runtime-only (see README.md's Configuration section)
- **System Requirements**: any OS/arch Go 1.25 supports (this project cross-builds linux/windows/darwin amd64+arm64 via `make build-all`); no special memory/disk requirements beyond a normal Go toolchain
- **Optional**: a local Docker daemon, only if running `make test-integration` (not required for the default build/test path)

## Build Steps

### 1. Install Dependencies
```bash
go mod download
```
(`make deps` also runs `go mod tidy` afterward.)

### 2. Configure Environment
No environment configuration is needed to build. To run the server
afterward, see README.md's "Configuration" section (`OPCUA_ENDPOINT` is
the one variable most deployments need to set).

### 3. Build All Units
```bash
go build ./...
# or: make build          (single binary for the host platform, to build/opcua-mcp)
# or: make build-all       (linux/windows/darwin, amd64+arm64)
```

### 4. Verify Build Success
- **Expected Output**: no output on success (Go's convention); `make build`
  prints "Building opcua-mcp..." then produces `build/opcua-mcp`.
- **Build Artifacts**: `build/opcua-mcp` (or `build/opcua-mcp-<os>-<arch>`
  for `build-all`) - a single static-ish Go binary, no separate runtime
  assets.
- **Common Warnings**: none expected; `go vet ./...` (part of `make all`)
  should report no issues.

## Troubleshooting

### Build Fails with Dependency Errors
- **Cause**: a corrupted or incomplete module cache, or `go.sum` mismatch.
- **Solution**: `go clean -modcache && go mod download`, then retry.

### Build Fails with Compilation Errors
- **Cause**: this repo is expected to build cleanly at every commit on
  `main` (verified throughout this AIDLC run's construction stages via
  `go build ./... && go vet ./... && gofmt -l .` after every code
  generation step) - a compile error on a clean checkout of `main` would
  indicate an environment mismatch (wrong Go version) rather than a real
  source issue.
- **Solution**: confirm `go version` reports 1.25.x or later; re-clone if
  the working tree may have been left mid-edit.
