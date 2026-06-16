package contextpack

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/redact"
)

type ValidationError struct {
	Reason      domain.GatewayErrorReason
	SourceLabel string
	Message     string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.SourceLabel != "" {
		return fmt.Sprintf("%s: %s", e.SourceLabel, e.Message)
	}
	return e.Message
}

func Validate(userPrompt string, blocks []domain.ContextBlock) error {
	if byteLen(userPrompt) > domain.MaxPromptBytes {
		return validationError(domain.ReasonRequestTooLarge, "", "prompt exceeds maximum size")
	}
	if len(blocks) > domain.MaxContextBlocks {
		return validationError(domain.ReasonRequestTooLarge, "", "too many context blocks")
	}

	totalContentBytes := 0
	for _, block := range blocks {
		if err := validateBlock(block); err != nil {
			return err
		}
		totalContentBytes += byteLen(block.Content)
		if totalContentBytes > domain.MaxTotalContextBytes {
			return validationError(domain.ReasonRequestTooLarge, "", "total context exceeds maximum size")
		}
	}
	return nil
}

func IsSafeSourceURI(rawURI string) bool {
	if rawURI == "" || strings.ContainsFunc(rawURI, unicode.IsSpace) || looksLikeLocalPath(rawURI) {
		return false
	}
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == ""
}

func validateBlock(block domain.ContextBlock) error {
	if byteLen(block.SourceLabel) > domain.MaxSourceLabelBytes {
		return validationError(domain.ReasonRequestTooLarge, "", "context block source_label exceeds maximum size")
	}
	sourceLabel := strings.TrimSpace(block.SourceLabel)
	if sourceLabel == "" || sourceLabel != block.SourceLabel || isUnsafeSourceLabel(sourceLabel) {
		return validationError(domain.ReasonInvalidRequest, "", "context block source_label is invalid")
	}
	switch block.Kind {
	case domain.ContextBlockKindApplication, domain.ContextBlockKindUntrusted:
	default:
		return validationError(domain.ReasonInvalidEnum, sourceLabel, "context block kind is invalid")
	}
	if byteLen(block.SourceURI) > domain.MaxSourceURIBytes {
		return validationError(domain.ReasonRequestTooLarge, sourceLabel, "context block source_uri exceeds maximum size")
	}
	if block.SourceURI != "" && !IsSafeSourceURI(block.SourceURI) {
		return validationError(domain.ReasonInvalidRequest, sourceLabel, "context block source_uri is invalid")
	}
	if byteLen(block.MimeType) > domain.MaxMimeTypeBytes {
		return validationError(domain.ReasonRequestTooLarge, sourceLabel, "context block mime_type exceeds maximum size")
	}
	if block.MimeType != "" && (redact.ContainsSecretLike(block.MimeType) || !isConservativeMimeType(block.MimeType)) {
		return validationError(domain.ReasonInvalidRequest, sourceLabel, "context block mime_type is invalid")
	}
	if byteLen(block.Content) > domain.MaxContextBlockContentBytes {
		return validationError(domain.ReasonRequestTooLarge, sourceLabel, "context block content exceeds maximum size")
	}
	if err := validateSourceContentLines(block.Content, sourceLabel); err != nil {
		return err
	}
	if redact.ContainsSecretLike(block.Content) {
		return validationError(domain.ReasonInvalidRequest, sourceLabel, "context block contains secret-like content")
	}
	return nil
}

func isUnsafeSourceLabel(value string) bool {
	if redact.ContainsSecretLike(value) || looksLikeLocalPath(value) || looksLikePathLabel(value) {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	return parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Scheme == "file"
}

func looksLikePathLabel(value string) bool {
	if strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") ||
		strings.HasPrefix(value, `.\\`) || strings.HasPrefix(value, `..\\`) {
		return true
	}
	return strings.ContainsAny(value, `/\`)
}

func isConservativeMimeType(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return false
	}
	return isMimeToken(parts[0]) && isMimeToken(parts[1])
}

func isMimeToken(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func validateSourceContentLines(content string, sourceLabel string) error {
	lineStart := 0
	for index := 0; index < len(content); index++ {
		if content[index] != '\n' {
			continue
		}
		lineEnd := index
		if lineEnd > lineStart && content[lineEnd-1] == '\r' {
			lineEnd--
		}
		if lineEnd-lineStart > domain.MaxContextSourceLineBytes {
			return validationError(domain.ReasonRequestTooLarge, sourceLabel, "context block source content line exceeds maximum size")
		}
		lineStart = index + 1
	}
	lineEnd := len(content)
	if lineEnd > lineStart && content[lineEnd-1] == '\r' {
		lineEnd--
	}
	if lineEnd-lineStart > domain.MaxContextSourceLineBytes {
		return validationError(domain.ReasonRequestTooLarge, sourceLabel, "context block source content line exceeds maximum size")
	}
	return nil
}

func looksLikeLocalPath(value string) bool {
	if strings.HasPrefix(value, `\\`) || filepath.IsAbs(value) {
		return true
	}
	if strings.HasPrefix(value, "/") {
		return true
	}
	if len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	return strings.HasPrefix(value, "~")
}

func validationError(reason domain.GatewayErrorReason, sourceLabel string, message string) *ValidationError {
	return &ValidationError{
		Reason:      reason,
		SourceLabel: sourceLabel,
		Message:     message,
	}
}

func byteLen(value string) int {
	if !utf8.ValidString(value) {
		return len([]rune(value))
	}
	return len(value)
}
