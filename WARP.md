# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Project Overview

Instant Launcher is a Go application that manages multiple Minecraft installations and launches them. It provides a graphical launcher (Gio) and a command-line interface; the Bubble Tea TUI was removed in v2.

It resolves versions, downloads libraries, assets and natives into a store shared across instances, picks a matching Java runtime and builds the launch command line itself.

**Tech stack**: Go, Cobra (CLI), Viper (config), Gio (GUI)

## Core Architecture

### Instances side by side
The system works by:
1. Storing instances in the instances directory (`<config dir>/instant-launcher/instances` by default)
2. Launching the game with `--gameDir` set to the instance itself, so there is no "active" instance and nothing to switch
3. Leaving the official launcher's `.minecraft` alone: it is only read, to import it as an instance (`import`, or the GUI's first-start wizard)

Versions before 2 replaced `.minecraft` with a link to the active instance.
`Manager.ReleaseLegacyLink` undoes that on startup: a link into the instances
directory is removed and the original put back from the old backup path.

### Directory Structure
```
instances/
├── Default/           # Imported from .minecraft
├── modpack-1.20.1/    # Modded instance
└── testing/           # Development instance
```

### Key components
- `cmd/instant-mc/` — the CLI, built with CGO_ENABLED=0 for every target
- `cmd/instant-launcher/` — the GUI; separate because Gio needs cgo
- `internal/instance/` — instances, their metadata and detection
- `internal/mojang/` — version manifests, inheritance, per-platform rules
- `internal/download/` — parallel, SHA-1 verified downloads
- `internal/java/` — runtime detection and selection
- `internal/launch/` — the shared store, preparation, arguments, the process
- `internal/launcher/` — application logic for the GUI; **imports no Gio**
- `internal/gui/` — Gio layout only
- `Makefile` — build, test, and `install` which registers the desktop entry

## Common Commands

### Development and Testing
```bash
# Build both binaries
make build

# Run tests (no display required)
make test

# Test the application locally
./instant-mc list

# Test all core functions
./instant-mc create test-instance
./instant-mc list
./instant-mc launch test-instance --offline Tester --dry-run

# Clean up test instance
./instant-mc delete test-instance
```

### Build and Release
```bash
# The CLI cross-compiles everywhere without cgo
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/instant-mc

# The GUI needs cgo on Linux and macOS, so it builds on a native runner.
# Windows is the exception: Gio is pure Go there.
make gui

# Install locally and register the desktop entry
make install
```

### Code Quality
```bash
# Run Go vet
go vet ./...

# Run tests with coverage
go test -v -cover ./...

# Format code
go fmt ./...

# Tidy dependencies
go mod tidy

# Check for updates
go list -u -m all
```

## Application Architecture

### Key types
- `instance.Manager` — instances, config and the import of `.minecraft`
- `instance.Meta` — per-instance settings in `instance.json`
- `mojang.Version` — a version manifest, both argument schemas
- `launch.Layout` — the shared store's paths
- `launch.Preparer` — resolve a version and fetch what it needs
- `java.Detector` — find and choose a runtime
- `launcher.Controller` / `launcher.Store` — GUI logic, Gio-free

### Safety mechanisms
- Never writes to the official launcher's `.minecraft`; an import copies from it, and a test checks it is unchanged afterwards
- A cancelled or failed import removes the half-made instance, and nothing else
- A running instance cannot be deleted or renamed
- Confirmation dialogs for destructive operations
- Proper error handling and user feedback

## Development Guidelines

### Code Style
- Follow Go conventions (gofmt, go vet, golint)
- Use meaningful package and function names
- Include comprehensive error handling with wrapped errors
- Add helpful user messages and feedback in both the CLI and the GUI
- Use structured logging when necessary

### Testing Approach
- Test with different Minecraft directory states (exists/doesn't exist)
- Test error conditions (invalid instance names, missing directories, permission issues)
- Verify an import leaves `.minecraft` exactly as it was
- Test mod/config/save counting accuracy
- Test CLI command parsing and validation

### File Structure Expectations
- Follow Go project layout standards
- Keep business logic in internal/instance package
- Use dependency injection for testability
- Keep CLI commands thin, delegating to business logic

## Platform Considerations

### Linux/macOS
- Path handling uses standard shell expansion
- macOS calls the official directory `minecraft`, without the dot

### Windows/WSL
- No symlinks or special privileges are needed: instances are plain folders
- Path separators and permissions might differ (file modes are not kept)
- The official launcher may hold files in `.minecraft` open; an import only reads, so that does not matter

## Instance Management Patterns

### Development Workflow
- Create clean test instances for mod development
- Use descriptive naming (e.g., `dev-1.20.1`, `compatibility-test`)
- Keep separate instances for different Minecraft versions

### Backup Strategy
- The official launcher's `.minecraft` is never modified
- Create dated backup instances before major changes (`create <name> --clone <instance> --with-saves`)
- Use tar/zip for sharing instances between systems

### Mod Organization
- Each instance maintains its own mods/ directory
- Mod counts are displayed for quick reference
- Easy to add/remove mods per instance

## Architectural rules

1. **`internal/launcher` must never import Gio.** The application logic lives
   there so it can be tested without a display; breaking the rule turns a
   headless CI run into a build failure, which is the point.
2. **Nothing in `internal/gui/screen_*.go` performs I/O.** No `os`, no
   `net/http`, no `exec`. Screens dispatch actions; the controller does the work.
3. **`Controller.Dispatch` never blocks.** A blocked render loop is a frozen
   window.
4. **Progress is sampled, repaints are coalesced.** An asset index has ~4000
   objects; per-item events would swamp the frame budget.
5. **Reading never writes.** `LoadMeta` on an instance with no `instance.json`
   returns defaults and touches nothing, so `list` cannot migrate anything.

## Things that have bitten us

- Replacing `.minecraft` with a link to the active instance (v1) meant renaming
  a directory the official launcher, Windows and virus scanners hold open. On
  Windows that fails with "Access is denied" at the worst moment. Instances now
  run in their own folders, and `.minecraft` is only read.

- `copyFile` once ended in `os.WriteFile(dst, data, 0644)`, which stripped the
  executable bit from bundled Java runtimes and left instances unable to run
  their own JVM. Preserve the source mode; `fsutil_test.go` guards it.
- `versions/` contains loose files (`jre_manifest.json`,
  `version_manifest_v2.json`) alongside the version directories. Filter them.
- A launcher profile's display name lies; `lastVersionId` is authoritative.
- Merging a loader profile must put its libraries *before* the parent's and
  dedupe keeping the first, or the game dies with `NoSuchMethodError`.
- An unknown `${token}` in the arguments must survive verbatim. Blanking it
  turns a readable error into an opaque crash.
