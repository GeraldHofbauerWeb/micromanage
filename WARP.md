# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Project Overview

MicroManage is a Go application that manages multiple Minecraft installations and launches them. It provides a graphical launcher (Gio) and a command-line interface; the Bubble Tea TUI was removed in v2.

It resolves versions, downloads libraries, assets and natives into a store shared across instances, picks a matching Java runtime and builds the launch command line itself. Switching between instances still uses symlinks.

**Tech stack**: Go, Cobra (CLI), Viper (config), Gio (GUI)

## Core Architecture

### Symlink-Based Design
The system works by:
1. Storing instances in `~/.minecraft-instances/`
2. Creating symlinks from `~/.minecraft` to the active instance
3. Backing up the original `.minecraft` directory to `.minecraft.backup`

### Directory Structure
```
~/.minecraft-instances/
├── vanilla/           # Clean Minecraft instance
├── modpack-1.20.1/   # Modded instance
└── testing/          # Development instance
```

### Key components
- `cmd/micromanage/` — the CLI, built with CGO_ENABLED=0 for every target
- `cmd/micromanage-launcher/` — the GUI; separate because Gio needs cgo
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
./micromanage list

# Test all core functions
./micromanage create test-instance
./micromanage switch test-instance
./micromanage list
./micromanage restore

# Clean up test instance
./micromanage delete test-instance
```

### Build and Release
```bash
# The CLI cross-compiles everywhere without cgo
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/micromanage

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
- `instance.Manager` — instances, config and the symlink
- `instance.Meta` — per-instance settings in `instance.json`
- `mojang.Version` — a version manifest, both argument schemas
- `launch.Layout` — the shared store's paths
- `launch.Preparer` — resolve a version and fetch what it needs
- `java.Detector` — find and choose a runtime
- `launcher.Controller` / `launcher.Store` — GUI logic, Gio-free

### Safety mechanisms
- Always backs up current .minecraft before switching
- Validates instance exists before switching  
- Uses symlinks (non-destructive, easily reversible)
- Provides restore functionality
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
- Verify symlink creation and backup functionality  
- Test mod/config/save counting accuracy
- Test CLI command parsing and validation

### File Structure Expectations
- Follow Go project layout standards
- Keep business logic in internal/instance package
- Use dependency injection for testability
- Keep CLI commands thin, delegating to business logic

## Platform Considerations

### Linux/macOS
- Uses standard Unix tools (ln, mv, cp, find)
- Symlink behavior is consistent
- Path handling uses standard shell expansion

### Windows/WSL
- May require different symlink handling
- Path separators and permissions might differ
- Consider Windows Minecraft launcher behavior

## Instance Management Patterns

### Development Workflow
- Create clean test instances for mod development
- Use descriptive naming (e.g., `dev-1.20.1`, `compatibility-test`)
- Keep separate instances for different Minecraft versions

### Backup Strategy
- Original .minecraft is always preserved as .minecraft.backup
- Create dated backup instances before major changes
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
