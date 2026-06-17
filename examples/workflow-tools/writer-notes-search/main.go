package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type searchResult struct {
	SourceID  string `json:"source_id"`
	Title     string `json:"title"`
	URI       string `json:"uri"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Excerpt   string `json:"excerpt"`
}

func main() {
	fixtureDir := flag.String("fixture-dir", filepath.FromSlash("examples/workflows/writer-notes/references"), "directory containing shipped writer note markdown files")
	query := flag.String("query", "", "case-insensitive text to find in shipped notes")
	limit := flag.Int("limit", 3, "maximum structured source results")
	flag.Parse()

	results, err := searchNotes(*fixtureDir, *query, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "writer_notes_search: %v\n", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintf(os.Stderr, "writer_notes_search: encode results: %v\n", err)
		os.Exit(1)
	}
}

func searchNotes(root string, query string, limit int) ([]searchResult, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("fixture-dir is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read fixture dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	needle := strings.ToLower(strings.TrimSpace(query))
	results := make([]searchResult, 0, limit)
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		fullPath := filepath.Join(root, entry.Name())
		contents, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		result, ok := sourceResultFromMarkdown(entry.Name(), string(contents), needle)
		if !ok {
			continue
		}
		results = append(results, result)
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func sourceResultFromMarkdown(name string, markdown string, needle string) (searchResult, bool) {
	lines := strings.Split(markdown, "\n")
	title := strings.TrimSuffix(name, filepath.Ext(name))
	sourceID := strings.TrimSuffix(name, filepath.Ext(name))
	uri := "writer-notes://" + sourceID
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "# "):
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		case strings.HasPrefix(strings.ToLower(trimmed), "source id:"):
			sourceID = strings.TrimSpace(strings.TrimPrefix(trimmed, "Source ID:"))
		case strings.HasPrefix(strings.ToLower(trimmed), "stable uri:"):
			uri = strings.TrimSpace(strings.TrimPrefix(trimmed, "Stable URI:"))
		}
	}

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(lower, "source id:") || strings.HasPrefix(lower, "stable uri:") {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(trimmed), needle) {
			continue
		}
		return searchResult{
			SourceID:  sourceID,
			Title:     title,
			URI:       uri,
			LineStart: index + 1,
			LineEnd:   index + 1,
			Excerpt:   trimmed,
		}, true
	}
	return searchResult{}, false
}
