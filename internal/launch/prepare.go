package launch

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/download"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/mojang"
)

// Phase names the stage a preparation is in, so a UI can show progress per
// step rather than as one opaque bar.
type Phase string

const (
	PhaseMetadata  Phase = "metadata"
	PhaseClient    Phase = "client"
	PhaseLibraries Phase = "libraries"
	PhaseNatives   Phase = "natives"
	PhaseAssets    Phase = "assets"
	PhaseLogging   Phase = "logging"
)

// Event reports progress within a phase.
type Event struct {
	Phase    Phase
	Progress download.Progress
	Message  string
	Done     bool
}

// Observer receives preparation events. It is called from worker goroutines,
// so implementations must not block.
type Observer func(Event)

// Preparer resolves a version and makes sure everything it needs is on disk.
type Preparer struct {
	Layout     *Layout
	Client     *mojang.Client
	Downloader *download.Downloader
	Platform   mojang.Platform
	// Features gates rule-controlled libraries and arguments.
	Features map[string]bool
	Observer Observer
}

// NewPreparer wires a Preparer against a shared store.
func NewPreparer(layout *Layout, d *download.Downloader) *Preparer {
	if d == nil {
		d = download.New()
	}
	return &Preparer{
		Layout:     layout,
		Client:     mojang.NewClient(d, layout.Versions(), layout.Cache()),
		Downloader: d,
		Platform:   mojang.CurrentPlatform(),
	}
}

// Prepared is everything a launch needs, once the files are in place.
type Prepared struct {
	Version *mojang.Version
	// Classpath entries in launch order, absolute.
	Classpath []string
	// NativesDir holds the extracted native libraries.
	NativesDir string
	// AssetsDir and AssetIndexID locate the asset store.
	AssetsDir    string
	AssetIndexID string
	// LibrariesDir is the maven root; Forge and NeoForge template it into
	// their module arguments as ${library_directory}.
	LibrariesDir string
	// GameAssetsDir is the materialised legacy tree, empty for modern versions.
	GameAssetsDir string
	// LoggingArgument is the log4j JVM flag with ${path} substituted.
	LoggingArgument string
}

func (p *Preparer) emit(e Event) {
	if p.Observer != nil {
		p.Observer(e)
	}
}

// Prepare resolves a version and downloads everything it requires.
func (p *Preparer) Prepare(ctx context.Context, versionID string) (*Prepared, error) {
	p.emit(Event{Phase: PhaseMetadata, Message: "Resolving " + versionID})

	version, err := p.Client.Resolve(ctx, versionID)
	if err != nil {
		return nil, err
	}
	p.emit(Event{Phase: PhaseMetadata, Done: true})

	out := &Prepared{
		Version:      version,
		NativesDir:   p.Layout.Natives(version.ID),
		AssetsDir:    p.Layout.Assets(),
		LibrariesDir: p.Layout.Libraries(),
	}

	if err := p.ensureClient(ctx, version); err != nil {
		return nil, err
	}
	if err := p.ensureLibraries(ctx, version, out); err != nil {
		return nil, err
	}
	if err := p.ensureNatives(ctx, version, out); err != nil {
		return nil, err
	}
	if err := p.ensureAssets(ctx, version, out); err != nil {
		return nil, err
	}
	if err := p.ensureLogging(ctx, version, out); err != nil {
		return nil, err
	}

	return out, nil
}

// ensureClient downloads the version's client jar.
func (p *Preparer) ensureClient(ctx context.Context, v *mojang.Version) error {
	client, ok := v.Downloads["client"]
	if !ok || client.URL == "" {
		// A loader profile that inherits properly always has one; a manifest
		// without it cannot be launched.
		return fmt.Errorf("version %s has no client download", v.ID)
	}

	p.emit(Event{Phase: PhaseClient, Message: "client jar"})
	item := download.Item{
		URL:  client.URL,
		Path: p.Layout.VersionJar(v.ID),
		SHA1: client.SHA1,
		Size: client.Size,
	}
	if err := p.Downloader.Fetch(ctx, item); err != nil {
		return fmt.Errorf("downloading the client jar: %w", err)
	}
	p.emit(Event{Phase: PhaseClient, Done: true})
	return nil
}

// ensureLibraries downloads every allowed non-native library and records the
// classpath in launch order.
func (p *Preparer) ensureLibraries(ctx context.Context, v *mojang.Version, out *Prepared) error {
	items, classpath, err := p.libraryItems(v)
	if err != nil {
		return err
	}

	// The client jar goes last so a library can never shadow game classes.
	out.Classpath = append(classpath, p.Layout.VersionJar(v.ID))

	if len(items) == 0 {
		return nil
	}

	p.emit(Event{Phase: PhaseLibraries, Message: fmt.Sprintf("%d libraries", len(items))})
	if err := p.runBatch(ctx, PhaseLibraries, items); err != nil {
		return fmt.Errorf("downloading libraries: %w", err)
	}
	p.emit(Event{Phase: PhaseLibraries, Done: true})
	return nil
}

// libraryItems selects the libraries this platform needs.
func (p *Preparer) libraryItems(v *mojang.Version) ([]download.Item, []string, error) {
	var items []download.Item
	var classpath []string

	// Deduplicate by group:artifact, keeping the first occurrence — the merge
	// already ordered loader libraries ahead of the vanilla ones.
	seen := map[string]bool{}

	for _, lib := range v.Libraries {
		if !mojang.Allowed(lib.Rules, p.Platform, p.Features) {
			continue
		}
		if lib.IsNative(p.Platform) {
			continue
		}

		key := mojang.MavenKey(lib.Name)
		if seen[key] {
			continue
		}
		seen[key] = true

		artifact, ok := lib.Artifact(p.Platform)
		if !ok {
			return nil, nil, fmt.Errorf("library %q has no resolvable artifact", lib.Name)
		}
		path := p.Layout.LibraryPath(artifact.Path)
		classpath = append(classpath, path)

		if artifact.URL != "" {
			items = append(items, download.Item{
				URL:  artifact.URL,
				Path: path,
				SHA1: artifact.SHA1,
				Size: artifact.Size,
			})
		}
	}
	return items, classpath, nil
}

// ensureNatives downloads and extracts the platform's native libraries.
func (p *Preparer) ensureNatives(ctx context.Context, v *mojang.Version, out *Prepared) error {
	type nativeArchive struct {
		item    download.Item
		exclude []string
	}

	var archives []nativeArchive
	var items []download.Item

	for _, lib := range v.Libraries {
		if !mojang.Allowed(lib.Rules, p.Platform, p.Features) {
			continue
		}
		if !lib.IsNative(p.Platform) {
			continue
		}

		artifact, ok := lib.Artifact(p.Platform)
		if !ok {
			continue
		}
		item := download.Item{
			URL:  artifact.URL,
			Path: p.Layout.LibraryPath(artifact.Path),
			SHA1: artifact.SHA1,
			Size: artifact.Size,
		}
		var exclude []string
		if lib.Extract != nil {
			exclude = lib.Extract.Exclude
		}
		archives = append(archives, nativeArchive{item: item, exclude: exclude})
		if artifact.URL != "" {
			items = append(items, item)
		}
	}

	if len(archives) == 0 {
		return nil
	}

	p.emit(Event{Phase: PhaseNatives, Message: fmt.Sprintf("%d native archives", len(archives))})
	if err := p.runBatch(ctx, PhaseNatives, items); err != nil {
		return fmt.Errorf("downloading natives: %w", err)
	}

	for _, a := range archives {
		if err := ExtractNatives(a.item.Path, out.NativesDir, a.exclude); err != nil {
			return fmt.Errorf("extracting %s: %w", filepath.Base(a.item.Path), err)
		}
	}
	p.emit(Event{Phase: PhaseNatives, Done: true})
	return nil
}

// ensureAssets downloads the asset index and its objects.
func (p *Preparer) ensureAssets(ctx context.Context, v *mojang.Version, out *Prepared) error {
	if v.AssetIndex == nil {
		return nil
	}
	out.AssetIndexID = v.AssetIndex.ID

	p.emit(Event{Phase: PhaseAssets, Message: "asset index " + v.AssetIndex.ID})
	index, err := p.Client.FetchAssetIndex(ctx, p.Layout.Assets(), v.AssetIndex)
	if err != nil {
		return err
	}

	items := index.DownloadItems(p.Layout.Assets())
	p.emit(Event{Phase: PhaseAssets, Message: fmt.Sprintf("%d objects", len(items))})
	if err := p.runBatch(ctx, PhaseAssets, items); err != nil {
		return fmt.Errorf("downloading assets: %w", err)
	}

	// Pre-1.7.3 versions read assets from a directory tree of logical names
	// rather than from the content-addressed store.
	if index.Virtual {
		out.GameAssetsDir = p.Layout.VirtualAssets()
		if err := index.MaterialiseVirtual(p.Layout.Assets(), out.GameAssetsDir); err != nil {
			return err
		}
	}

	p.emit(Event{Phase: PhaseAssets, Done: true})
	return nil
}

// ensureLogging downloads the log4j configuration a version pins. For 1.7 to
// 1.18 this is also what mitigates Log4Shell.
func (p *Preparer) ensureLogging(ctx context.Context, v *mojang.Version, out *Prepared) error {
	if v.Logging == nil || v.Logging.Client == nil || v.Logging.Client.File.URL == "" {
		return nil
	}

	client := v.Logging.Client
	name := client.File.Path
	if name == "" {
		name = filepath.Base(client.File.URL)
	}
	path := filepath.Join(p.Layout.LogConfigs(), filepath.Base(name))

	p.emit(Event{Phase: PhaseLogging, Message: filepath.Base(path)})
	item := download.Item{URL: client.File.URL, Path: path, SHA1: client.File.SHA1, Size: client.File.Size}
	if err := p.Downloader.Fetch(ctx, item); err != nil {
		return fmt.Errorf("downloading the logging config: %w", err)
	}

	out.LoggingArgument = replacePlaceholder(client.Argument, "path", path)
	p.emit(Event{Phase: PhaseLogging, Done: true})
	return nil
}

// runBatch downloads a set of items, forwarding sampled progress.
func (p *Preparer) runBatch(ctx context.Context, phase Phase, items []download.Item) error {
	if len(items) == 0 {
		return nil
	}

	// Swap in a reporter for the duration; the downloader is shared across
	// phases and each needs its own progress destination.
	previous := p.Downloader.Reporter
	p.Downloader.Reporter = func(pr download.Progress) {
		p.emit(Event{Phase: phase, Progress: pr})
	}
	defer func() { p.Downloader.Reporter = previous }()

	return p.Downloader.Run(ctx, items)
}
