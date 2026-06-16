package redact

import (
	"sort"
	"strings"
)

const maxStreamingCarryBytes = 2 * 1024

type Stream struct {
	redactor       *Redactor
	carry          string
	carryPublished int
}

func NewStream(redactor *Redactor) *Stream {
	return &Stream{redactor: redactor}
}

func (s *Stream) Write(chunk string) string {
	if s == nil {
		return chunk
	}
	if chunk == "" {
		return ""
	}

	window := s.carry + chunk
	hold := s.holdBytes(window)
	if hold == 0 {
		s.carry = ""
		s.carryPublished = 0
		return s.redactRange(window, s.carryPublished, len(window), nil)
	}
	if hold >= len(window) {
		s.carry = window
		s.carryPublished = min(s.carryPublished, len(s.carry))
		return ""
	}

	intervals := s.sensitiveIntervals(window)
	carryStart := len(window) - hold
	publishEnd := safePublishEnd(intervals, carryStart)
	emitStart := min(s.carryPublished, len(window))
	publish := s.redactRange(window, emitStart, publishEnd, intervals)
	emittedThrough := max(emitStart, publishEnd)
	s.carry = window[carryStart:]
	s.carryPublished = emittedThrough - carryStart
	return publish
}

func (s *Stream) Flush() string {
	if s == nil || s.carry == "" {
		return ""
	}
	carry := s.carry
	emitStart := min(s.carryPublished, len(carry))
	s.carry = ""
	s.carryPublished = 0
	return s.redactRange(carry, emitStart, len(carry), s.sensitiveIntervals(carry))
}

func (s *Stream) holdBytes(window string) int {
	if s.redactor == nil {
		return 0
	}
	longest := s.redactor.LongestSensitiveSegmentBytes()
	if longest <= 1 {
		return 0
	}
	hold := min(longest-1, maxStreamingCarryBytes)
	return min(hold, len(window))
}

func (s *Stream) redact(text string) string {
	if s.redactor == nil {
		return text
	}
	return s.redactor.RedactString(text)
}

type sensitiveInterval struct {
	start int
	end   int
}

func (s *Stream) sensitiveIntervals(window string) []sensitiveInterval {
	if s.redactor == nil || window == "" {
		return nil
	}

	var intervals []sensitiveInterval
	for _, segment := range s.redactor.SensitiveSegments() {
		searchStart := 0
		for {
			index := strings.Index(window[searchStart:], segment)
			if index < 0 {
				break
			}
			start := searchStart + index
			intervals = append(intervals, sensitiveInterval{
				start: start,
				end:   start + len(segment),
			})
			searchStart = start + 1
		}
	}
	sort.SliceStable(intervals, func(i, j int) bool {
		if intervals[i].start == intervals[j].start {
			return intervals[i].end > intervals[j].end
		}
		return intervals[i].start < intervals[j].start
	})
	return intervals
}

func safePublishEnd(intervals []sensitiveInterval, publishEnd int) int {
	for {
		nextPublishEnd := publishEnd
		for _, interval := range intervals {
			if interval.start < nextPublishEnd && interval.end > nextPublishEnd {
				nextPublishEnd = interval.end
			}
		}
		if nextPublishEnd == publishEnd {
			return publishEnd
		}
		publishEnd = nextPublishEnd
	}
}

func (s *Stream) redactRange(window string, start int, end int, intervals []sensitiveInterval) string {
	if start >= end {
		return ""
	}
	if s.redactor == nil || len(intervals) == 0 {
		return s.redact(window[start:end])
	}

	var builder strings.Builder
	cursor := start
	for _, interval := range intervals {
		if interval.end <= cursor || interval.start >= end {
			continue
		}
		rawEnd := min(max(interval.start, cursor), end)
		if cursor < rawEnd {
			builder.WriteString(s.redact(window[cursor:rawEnd]))
		}
		builder.WriteString(SensitiveValueMarker)
		cursor = min(max(cursor, interval.end), end)
	}
	if cursor < end {
		builder.WriteString(s.redact(window[cursor:end]))
	}
	return builder.String()
}
