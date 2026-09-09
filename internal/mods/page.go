package mods

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/GeraldHofbauerWeb/micromanage/internal/download"
)

// Endpoints are the services a mod's page is looked up on.
type Endpoints struct {
	ModrinthAPI string
	ModrinthWeb string
	// CurseForgeSearch is a web search URL taking the query in q=; there
	// is no keyless API, so the search page is the best that can be offered.
	CurseForgeSearch string
}

// DefaultEndpoints are the real sites.
var DefaultEndpoints = Endpoints{
	ModrinthAPI:      "https://api.modrinth.com/v2",
	ModrinthWeb:      "https://modrinth.com/mod/",
	CurseForgeSearch: "https://www.curseforge.com/minecraft/search?class=mc-mods&search=",
}

// Page is where a mod can be read about.
type Page struct {
	URL string
	// Site says which one, for the status line: "Modrinth" or "CurseForge".
	Site string
	// Exact is true when the file itself was recognised, false for a search
	// by name that may land on a namesake.
	Exact bool
}

// PageFinder resolves a mod file to its page.
type PageFinder struct {
	Endpoints  Endpoints
	Downloader *download.Downloader
}

// NewPageFinder returns a finder against the real sites.
func NewPageFinder(d *download.Downloader) *PageFinder {
	if d == nil {
		d = download.New()
	}
	return &PageFinder{Endpoints: DefaultEndpoints, Downloader: d}
}

// Find locates a mod's page. The file's SHA-1 is tried first — Modrinth
// indexes every file it hosts by hash, so a mod downloaded from there
// resolves to exactly its project. Failing that, the name from the jar's
// manifest is searched on Modrinth, and failing that too, the player is
// handed a CurseForge search for the name: a page that says "not found" is
// worse than a search that finds it.
func (f *PageFinder) Find(ctx context.Context, jarPath string, info Info) (Page, error) {
	if sha := download.FileSHA1(jarPath); sha != "" {
		if slug, ok := f.modrinthByHash(ctx, sha); ok {
			return Page{URL: f.Endpoints.ModrinthWeb + slug, Site: "Modrinth", Exact: true}, nil
		}
	}

	query := info.Name
	if query == "" {
		query = info.ID
	}
	if query == "" {
		query = guessName(jarPath)
	}
	if slug, ok := f.modrinthSearch(ctx, query); ok {
		return Page{URL: f.Endpoints.ModrinthWeb + slug, Site: "Modrinth"}, nil
	}
	if info.ID != "" && info.ID != query {
		if slug, ok := f.modrinthSearch(ctx, info.ID); ok {
			return Page{URL: f.Endpoints.ModrinthWeb + slug, Site: "Modrinth"}, nil
		}
	}
	return Page{URL: f.Endpoints.CurseForgeSearch + url.QueryEscape(query), Site: "CurseForge"}, nil
}

// modrinthByHash asks Modrinth which project a file belongs to.
func (f *PageFinder) modrinthByHash(ctx context.Context, sha1 string) (slug string, ok bool) {
	data, err := f.Downloader.GetJSON(ctx, f.Endpoints.ModrinthAPI+"/version_file/"+sha1, "")
	if err != nil {
		return "", false
	}
	var version struct {
		ProjectID string `json:"project_id"`
	}
	if json.Unmarshal(data, &version) != nil || version.ProjectID == "" {
		return "", false
	}
	// The project id works in a URL too, but the slug is what the site
	// shows and what a player would bookmark.
	data, err = f.Downloader.GetJSON(ctx, f.Endpoints.ModrinthAPI+"/project/"+version.ProjectID, "")
	if err != nil {
		return version.ProjectID, true
	}
	var project struct {
		Slug string `json:"slug"`
	}
	if json.Unmarshal(data, &project) != nil || project.Slug == "" {
		return version.ProjectID, true
	}
	return project.Slug, true
}

// modrinthSearch takes the first mod whose title matches the query
// closely enough to trust.
func (f *PageFinder) modrinthSearch(ctx context.Context, query string) (slug string, ok bool) {
	if strings.TrimSpace(query) == "" {
		return "", false
	}
	u := fmt.Sprintf("%s/search?query=%s&facets=%s&limit=5",
		f.Endpoints.ModrinthAPI, url.QueryEscape(query), url.QueryEscape(`[["project_type:mod"]]`))
	data, err := f.Downloader.GetJSON(ctx, u, "")
	if err != nil {
		return "", false
	}
	var result struct {
		Hits []struct {
			Slug  string `json:"slug"`
			Title string `json:"title"`
		} `json:"hits"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Hits) == 0 {
		return "", false
	}
	want := normalise(query)
	for _, h := range result.Hits {
		if normalise(h.Title) == want || normalise(h.Slug) == want {
			return h.Slug, true
		}
	}
	// No exact title; the top hit is Modrinth's best guess, which is
	// usually right for a distinctive name and wrong for a generic one.
	if len(want) >= 6 {
		return result.Hits[0].Slug, true
	}
	return "", false
}

// normalise strips everything but letters and digits, lower-cased, so
// "All The Leaks", "alltheleaks" and "All-The-Leaks" compare equal.
func normalise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// guessName turns "create-1.21.1-6.0.6.jar" into "create": the part before
// the first version-looking segment.
func guessName(jarPath string) string {
	name := jarPath
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".disabled"), ".jar")
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == '+' || r == ' ' })
	var kept []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		if p[0] >= '0' && p[0] <= '9' {
			break
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		return name
	}
	return strings.Join(kept, " ")
}
