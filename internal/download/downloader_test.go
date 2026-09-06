package download

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// newServer serves the given bodies by path and counts requests.
func newServer(t *testing.T, bodies map[string]string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestFetchWritesVerifiedFile(t *testing.T) {
	const body = "hello world"
	srv, _ := newServer(t, map[string]string{"/a.jar": body})
	dir := t.TempDir()

	d := New()
	item := Item{
		URL:  srv.URL + "/a.jar",
		Path: filepath.Join(dir, "a.jar"),
		SHA1: sha1Hex(body),
		Size: int64(len(body)),
	}
	if err := d.Fetch(context.Background(), item); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	got, err := os.ReadFile(item.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("content = %q, want %q", got, body)
	}
	// The temporary file must not survive a success.
	if _, err := os.Stat(item.Path + ".part"); !os.IsNotExist(err) {
		t.Error(".part file left behind after a successful download")
	}
}

// TestFetchSHA1MismatchLeavesNoFile is the integrity guarantee: a corrupted
// download must never end up at the target path, where its size alone would
// make it look valid on the next run.
func TestFetchSHA1MismatchLeavesNoFile(t *testing.T) {
	srv, _ := newServer(t, map[string]string{"/a.jar": "corrupted"})
	dir := t.TempDir()

	d := New()
	item := Item{
		URL:  srv.URL + "/a.jar",
		Path: filepath.Join(dir, "a.jar"),
		SHA1: sha1Hex("the real thing"),
	}
	err := d.Fetch(context.Background(), item)
	if err == nil {
		t.Fatal("Fetch with a bad digest = nil, want an error")
	}
	if !strings.Contains(err.Error(), "sha1 mismatch") {
		t.Errorf("error = %v, want it to mention the digest", err)
	}

	if _, err := os.Stat(item.Path); !os.IsNotExist(err) {
		t.Error("a file was left at the target path despite the mismatch")
	}
	if _, err := os.Stat(item.Path + ".part"); !os.IsNotExist(err) {
		t.Error("the rejected .part file was not removed")
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv, _ := newServer(t, nil)
	dir := t.TempDir()

	err := New().Fetch(context.Background(), Item{
		URL:  srv.URL + "/missing.jar",
		Path: filepath.Join(dir, "missing.jar"),
	})
	if err == nil {
		t.Fatal("Fetch of a 404 = nil, want an error")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "missing.jar")); !os.IsNotExist(statErr) {
		t.Error("a file was created for a failed request")
	}
}

// TestRunSkipsPresentFiles covers the cheap path taken on every launch: a file
// whose size already matches is not re-requested.
func TestRunSkipsPresentFiles(t *testing.T) {
	const body = "already here"
	srv, hits := newServer(t, map[string]string{"/a.jar": body})
	dir := t.TempDir()

	path := filepath.Join(dir, "a.jar")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	item := Item{URL: srv.URL + "/a.jar", Path: path, SHA1: sha1Hex(body), Size: int64(len(body))}
	if err := New().Run(context.Background(), []Item{item}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("made %d request(s) for a file already present", n)
	}
}

// TestRunRefetchesWrongSize covers a truncated leftover.
func TestRunRefetchesWrongSize(t *testing.T) {
	const body = "the full body"
	srv, hits := newServer(t, map[string]string{"/a.jar": body})
	dir := t.TempDir()

	path := filepath.Join(dir, "a.jar")
	if err := os.WriteFile(path, []byte("trunc"), 0o644); err != nil {
		t.Fatal(err)
	}

	item := Item{URL: srv.URL + "/a.jar", Path: path, SHA1: sha1Hex(body), Size: int64(len(body))}
	if err := New().Run(context.Background(), []Item{item}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("made %d request(s), want 1", hits.Load())
	}
	got, _ := os.ReadFile(path)
	if string(got) != body {
		t.Errorf("content = %q, want the full body", got)
	}
}

// TestVerifyDetectsCorruption covers the opt-in deep check: same size, wrong
// content.
func TestVerifyDetectsCorruption(t *testing.T) {
	const body = "good content"
	srv, hits := newServer(t, map[string]string{"/a.jar": body})
	dir := t.TempDir()

	path := filepath.Join(dir, "a.jar")
	corrupt := strings.Repeat("x", len(body))
	if err := os.WriteFile(path, []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}

	item := Item{URL: srv.URL + "/a.jar", Path: path, SHA1: sha1Hex(body), Size: int64(len(body))}

	// Without Verify the size match is enough, so nothing is fetched.
	if err := New().Run(context.Background(), []Item{item}); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Error("a size-matching file was re-fetched without Verify")
	}

	d := New()
	d.Verify = true
	if err := d.Run(context.Background(), []Item{item}); err != nil {
		t.Fatalf("Run with Verify: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("Verify made %d request(s), want 1", hits.Load())
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Error("Verify did not repair the corrupted file")
	}
}

func TestRunReportsProgressToCompletion(t *testing.T) {
	bodies := map[string]string{}
	var items []Item
	dir := t.TempDir()
	for i := range 20 {
		p := fmt.Sprintf("/f%d.jar", i)
		body := strings.Repeat("x", 100)
		bodies[p] = body
		items = append(items, Item{Path: filepath.Join(dir, fmt.Sprintf("f%d.jar", i)), SHA1: sha1Hex(body), Size: 100})
	}
	srv, _ := newServer(t, bodies)
	for i := range items {
		items[i].URL = srv.URL + fmt.Sprintf("/f%d.jar", i)
	}

	var last Progress
	var updates atomic.Int64
	d := New()
	d.Reporter = func(p Progress) {
		updates.Add(1)
		last = p
	}

	if err := d.Run(context.Background(), items); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if last.FilesDone != 20 || last.FilesTotal != 20 {
		t.Errorf("final progress = %d/%d files, want 20/20", last.FilesDone, last.FilesTotal)
	}
	if last.BytesDone != last.BytesTotal || last.BytesTotal != 2000 {
		t.Errorf("final progress = %d/%d bytes, want 2000/2000", last.BytesDone, last.BytesTotal)
	}
	if last.Percent() != 100 {
		t.Errorf("final percent = %v, want 100", last.Percent())
	}
	// Progress is sampled, so a 20-file batch must not produce 20 updates.
	if n := updates.Load(); n > 5 {
		t.Errorf("got %d progress updates for 20 files; sampling is not working", n)
	}
}

// TestRunRespectsWorkerLimit guards against flooding the CDN.
func TestRunRespectsWorkerLimit(t *testing.T) {
	var inFlight, peak atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)
		fmt.Fprint(w, "body")
	}))
	defer srv.Close()

	dir := t.TempDir()
	var items []Item
	for i := range 30 {
		items = append(items, Item{
			URL:  fmt.Sprintf("%s/f%d", srv.URL, i),
			Path: filepath.Join(dir, fmt.Sprintf("f%d", i)),
			Size: 4,
		})
	}

	d := New()
	d.Workers = 4
	if err := d.Run(context.Background(), items); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak.Load() > 4 {
		t.Errorf("peak concurrency %d exceeded the limit of 4", peak.Load())
	}
}

// TestRunCancellation checks that a cancelled batch reports the cancellation
// and leaves a resumable .part file behind.
func TestRunCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "never finishes")
	}))
	defer srv.Close()
	defer close(release)

	dir := t.TempDir()
	items := []Item{{URL: srv.URL + "/slow", Path: filepath.Join(dir, "slow.jar"), Size: 100}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := New().Run(ctx, items)
	if err == nil {
		t.Fatal("a cancelled Run returned nil, want an error")
	}

	if _, statErr := os.Stat(items[0].Path); !os.IsNotExist(statErr) {
		t.Error("the target file exists although the download was cancelled")
	}
	// The partial file is deliberately kept so a resumed run can see it.
	if _, statErr := os.Stat(items[0].Path + ".part"); statErr != nil {
		t.Error("no .part file left for a resumed run")
	}
}

func TestRunEmptyBatch(t *testing.T) {
	if err := New().Run(context.Background(), nil); err != nil {
		t.Errorf("Run(nil) = %v, want nil", err)
	}
}

func TestGetJSONVerifiesDigest(t *testing.T) {
	const body = `{"id":"1.21.1"}`
	srv, _ := newServer(t, map[string]string{"/v.json": body})

	d := New()
	got, err := d.GetJSON(context.Background(), srv.URL+"/v.json", sha1Hex(body))
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q", got)
	}

	if _, err := d.GetJSON(context.Background(), srv.URL+"/v.json", sha1Hex("something else")); err == nil {
		t.Error("GetJSON accepted a body with the wrong digest")
	}
}

func TestFileSHA1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FileSHA1(path); got != sha1Hex("content") {
		t.Errorf("FileSHA1 = %q", got)
	}
	if got := FileSHA1(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("FileSHA1 of a missing file = %q, want empty", got)
	}
}
