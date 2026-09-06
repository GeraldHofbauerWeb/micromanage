// Package download fetches files in parallel with integrity verification.
package download

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultWorkers is the default download concurrency. An asset index has
// thousands of small objects, so the limit is about not hammering Mojang's CDN
// rather than about local throughput.
const DefaultWorkers = 16

// UserAgent identifies this launcher to the services it talks to.
const UserAgent = "minecraft-instance-manager/2.0"

// Item is one file to fetch.
type Item struct {
	URL  string
	Path string // absolute destination
	SHA1 string // expected digest; empty skips verification
	Size int64  // expected size; 0 means unknown
}

// Progress is a snapshot of an in-flight batch.
type Progress struct {
	FilesDone  int64
	FilesTotal int64
	BytesDone  int64
	BytesTotal int64
	Current    string
}

// Percent returns completion as a value between 0 and 100, by bytes when the
// total is known and by file count otherwise.
func (p Progress) Percent() float64 {
	switch {
	case p.BytesTotal > 0:
		return float64(p.BytesDone) / float64(p.BytesTotal) * 100
	case p.FilesTotal > 0:
		return float64(p.FilesDone) / float64(p.FilesTotal) * 100
	}
	return 0
}

// Reporter receives sampled progress updates.
type Reporter func(Progress)

// SampleInterval is how often a running batch reports progress. Emitting per
// file would mean thousands of events for one asset index, which is enough to
// swamp a UI's render loop.
const SampleInterval = 100 * time.Millisecond

// Downloader fetches batches of items.
type Downloader struct {
	Client   *http.Client
	Workers  int
	Reporter Reporter

	// Verify forces a full SHA-1 check of files that are already present.
	// Off by default: re-hashing a gigabyte of assets on every launch costs
	// far more than it catches.
	Verify bool
}

// New returns a Downloader with sensible defaults.
func New() *Downloader {
	return &Downloader{
		Client: &http.Client{
			// Generous: some Mojang library mirrors are slow, and a batch is
			// cancelled through the context rather than by timing out.
			Timeout: 5 * time.Minute,
		},
		Workers: DefaultWorkers,
	}
}

// counters tracks a batch's progress across workers without locking.
type counters struct {
	filesDone  atomic.Int64
	filesTotal int64
	bytesDone  atomic.Int64
	bytesTotal int64
	current    atomic.Pointer[string]
}

func (c *counters) snapshot() Progress {
	p := Progress{
		FilesDone:  c.filesDone.Load(),
		FilesTotal: c.filesTotal,
		BytesDone:  c.bytesDone.Load(),
		BytesTotal: c.bytesTotal,
	}
	if s := c.current.Load(); s != nil {
		p.Current = *s
	}
	return p
}

// Run fetches every item, skipping those already present and valid.
//
// Progress is sampled on a ticker rather than emitted per file, so the caller
// sees a bounded event rate no matter how many items there are.
func (d *Downloader) Run(parent context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}

	c := &counters{filesTotal: int64(len(items))}
	for _, it := range items {
		c.bytesTotal += it.Size
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	var samplerDone sync.WaitGroup
	if d.Reporter != nil {
		samplerDone.Add(1)
		go func() {
			defer samplerDone.Done()
			ticker := time.NewTicker(SampleInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					d.Reporter(c.snapshot())
				}
			}
		}()
	}

	workers := d.Workers
	if workers <= 0 {
		workers = DefaultWorkers
	}
	if workers > len(items) {
		workers = len(items)
	}

	jobs := make(chan Item)
	errs := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}
				name := filepath.Base(item.Path)
				c.current.Store(&name)

				written, err := d.fetch(ctx, item)
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
				if item.Size > 0 {
					c.bytesDone.Add(item.Size)
				} else {
					c.bytesDone.Add(written)
				}
				c.filesDone.Add(1)
			}
		}()
	}

feed:
	for _, item := range items {
		select {
		case jobs <- item:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	cancel()
	samplerDone.Wait()

	select {
	case err := <-errs:
		return err
	default:
	}
	// Run cancels its own context on the way out, so only the parent's state
	// distinguishes "caller gave up" from "finished".
	if err := parent.Err(); err != nil {
		return err
	}

	if d.Reporter != nil {
		d.Reporter(c.snapshot())
	}
	return nil
}

// Fetch downloads a single item.
func (d *Downloader) Fetch(ctx context.Context, item Item) error {
	_, err := d.fetch(ctx, item)
	return err
}

// fetch downloads one item unless it is already present and valid, returning
// the number of bytes written.
func (d *Downloader) fetch(ctx context.Context, item Item) (int64, error) {
	if d.isPresent(item) {
		return 0, nil
	}

	if err := os.MkdirAll(filepath.Dir(item.Path), 0o755); err != nil {
		return 0, err
	}

	// Download beside the target and rename on success, so an interrupted
	// download never leaves a truncated file that later looks valid.
	part := item.Path + ".part"
	out, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}

	written, digest, err := d.stream(ctx, item.URL, out)
	closeErr := out.Close()
	if err != nil {
		// Leave the .part file: a resumed run overwrites it, and keeping it
		// makes an interrupted batch visible.
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}

	if item.SHA1 != "" && digest != item.SHA1 {
		os.Remove(part)
		return 0, fmt.Errorf("%s: sha1 mismatch (got %s, want %s)",
			filepath.Base(item.Path), digest, item.SHA1)
	}

	if err := os.Rename(part, item.Path); err != nil {
		return 0, err
	}
	return written, nil
}

// stream copies the response body into w while hashing it.
func (d *Downloader) stream(ctx context.Context, url string, w io.Writer) (int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("User-Agent", UserAgent)

	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	h := sha1.New()
	n, err := io.Copy(io.MultiWriter(w, h), resp.Body)
	if err != nil {
		return n, "", fmt.Errorf("reading %s: %w", url, err)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// isPresent reports whether the destination already holds the wanted file.
//
// The size check is the cheap path taken on every launch; a full digest is
// only computed when Verify is set, because hashing every asset object costs
// seconds to minutes.
func (d *Downloader) isPresent(item Item) bool {
	info, err := os.Stat(item.Path)
	if err != nil || info.IsDir() {
		return false
	}
	if item.Size > 0 && info.Size() != item.Size {
		return false
	}
	if item.Size == 0 && info.Size() == 0 {
		// Nothing to compare against; re-fetch rather than trust an empty file.
		return false
	}
	if d.Verify && item.SHA1 != "" {
		return FileSHA1(item.Path) == item.SHA1
	}
	return true
}

// FileSHA1 returns a file's hex-encoded SHA-1, or "" if it cannot be read.
func FileSHA1(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// GetJSON fetches a URL into memory, optionally verifying its digest.
func (d *Downloader) GetJSON(ctx context.Context, url, wantSHA1 string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)

	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if wantSHA1 != "" {
		sum := sha1.Sum(data)
		if got := hex.EncodeToString(sum[:]); got != wantSHA1 {
			return nil, fmt.Errorf("%s: sha1 mismatch (got %s, want %s)", url, got, wantSHA1)
		}
	}
	return data, nil
}
