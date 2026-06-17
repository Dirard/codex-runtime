package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWriterNotesSearchReturnsStructuredSourceProof(t *testing.T) {
	root := filepath.Join("..", "..", "workflows", "writer-notes", "references")
	results, err := searchNotes(root, "harbor", 2)
	if err != nil {
		t.Fatalf("searchNotes returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1: %#v", len(results), results)
	}
	result := results[0]
	if result.SourceID != "harbor-fire" || result.Title != "Harbor Fire Notes" || result.URI != "writer-notes://harbor-fire" {
		t.Fatalf("source metadata = %#v", result)
	}
	if result.LineStart <= 0 || !strings.Contains(result.Excerpt, "harbor warehouse fire") {
		t.Fatalf("source span/excerpt = %#v", result)
	}
}

func TestWriterNotesSearchReportsNoSourcesWhenNothingMatches(t *testing.T) {
	root := filepath.Join("..", "..", "workflows", "writer-notes", "references")
	results, err := searchNotes(root, "nonexistent-topic", 2)
	if err != nil {
		t.Fatalf("searchNotes returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want no sources", results)
	}
}
