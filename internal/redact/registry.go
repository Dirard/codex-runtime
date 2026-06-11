package redact

import (
	"sort"
	"strings"
	"sync"
)

const (
	minSensitiveValueBytes   = 4
	maxSensitiveSegmentBytes = 2 * 1024

	SensitiveValueMarker = "[REDACTED:sensitive-value]"
)

// Registry stores exact sensitive byte segments for one task or one app-server
// connection. It never exposes prefixes, suffixes, hashes, or lengths.
type Registry struct {
	mu       sync.RWMutex
	segments []string
	seen     map[string]struct{}
	longest  int
}

func NewRegistry() *Registry {
	return &Registry{
		seen: map[string]struct{}{},
	}
}

func (r *Registry) Add(value string) {
	if r == nil || len(value) < minSensitiveValueBytes {
		return
	}

	for _, segment := range splitSensitiveValue(value) {
		r.addSegment(segment)
	}
}

func (r *Registry) AddMany(values ...string) {
	for _, value := range values {
		r.Add(value)
	}
}

func (r *Registry) Redact(text string) string {
	if r == nil || text == "" {
		return text
	}

	return redactSegmentsLongestFirst(text, r.Segments())
}

func (r *Registry) Segments() []string {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return append([]string(nil), r.segments...)
}

func (r *Registry) LongestSegmentBytes() int {
	if r == nil {
		return 0
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.longest
}

func (r *Registry) addSegment(segment string) {
	if len(segment) < minSensitiveValueBytes {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.seen == nil {
		r.seen = map[string]struct{}{}
	}
	if _, exists := r.seen[segment]; exists {
		return
	}
	r.seen[segment] = struct{}{}
	r.segments = append(r.segments, segment)
	r.longest = max(r.longest, len(segment))
}

func splitSensitiveValue(value string) []string {
	if len(value) < minSensitiveValueBytes {
		return nil
	}
	if len(value) <= maxSensitiveSegmentBytes {
		return []string{value}
	}

	var segments []string
	for start := 0; start < len(value); {
		remaining := len(value) - start
		size := min(maxSensitiveSegmentBytes, remaining)
		if remaining > maxSensitiveSegmentBytes {
			remainder := remaining - size
			if remainder > 0 && remainder < minSensitiveValueBytes {
				size -= minSensitiveValueBytes - remainder
			}
		}
		segments = append(segments, value[start:start+size])
		start += size
	}
	return segments
}

func redactSegmentsLongestFirst(text string, segments []string) string {
	if text == "" || len(segments) == 0 {
		return text
	}

	ordered := append([]string(nil), segments...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return len(ordered[i]) > len(ordered[j])
	})
	for _, segment := range ordered {
		text = strings.ReplaceAll(text, segment, SensitiveValueMarker)
	}
	return text
}
