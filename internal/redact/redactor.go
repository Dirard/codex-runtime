package redact

import (
	"net/url"
	"regexp"
	"strings"
)

const (
	StructuredSecretMarker = "[REDACTED:structured-secret]"
	PathMarker             = "[REDACTED:path]"

	jsonSecretMarker       = "[REDACTED:secret]"
	jsonStringScalarRE     = `(?:\\)?"(?:[^"\\\r\n]|\\.)*(?:\\)?"`
	jsonNonNullScalarRE    = `(?:` + jsonStringScalarRE + `|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|true|false)`
	genericSecretKeyPartRE = `(?:token|secret|password|passwd|api_key|apikey|access_key|refresh_token|session_key)`
)

var (
	privateKeyBlockRE        = regexp.MustCompile(`(?is)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)
	authorizationRE          = regexp.MustCompile(`(?i)\b(authorization\s*[:=]\s*bearer\s+)[^\s"',;}]+`)
	cookieHeaderRE           = regexp.MustCompile(`(?i)\b((?:set-cookie|cookie)\s*[:=]\s*)[^\r\n]+`)
	sessionCookieRE          = regexp.MustCompile(`(?i)\b((?:session|csrf)\s*=\s*)[^\s"',;}]+`)
	jwtRE                    = regexp.MustCompile(`\b[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{12,}\b`)
	openAIKeyRE              = regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{16,}\b`)
	jsonSecretFieldRE        = regexp.MustCompile(`(?i)((?:\\)?"(?:accessToken|access_token|chatgptAccountId|chatgpt_account_id|chatgptPlanType|chatgpt_plan_type)(?:\\)?"\s*:\s*)(` + jsonNonNullScalarRE + `)`)
	jsonGenericSecretFieldRE = regexp.MustCompile(`(?i)((?:\\)?"[A-Za-z0-9_.-]*` + genericSecretKeyPartRE + `[A-Za-z0-9_.-]*(?:\\)?"\s*:\s*)(` + jsonNonNullScalarRE + `)`)
	genericAssignRE          = regexp.MustCompile(`(?i)\b([A-Za-z0-9_.-]*(?:token|secret|password|passwd|api_key|apikey|access_key|refresh_token|session_key)[A-Za-z0-9_.-]*\s*[:=]\s*)("(?:[^"\\\r\n]|\\.)*"|'(?:[^'\\\r\n]|\\.)*'|[^\r\n,;}]+)`)
	urlCandidateRE           = regexp.MustCompile(`(?i)\b[A-Za-z][A-Za-z0-9+.-]*:(?://)?[^\s"'<>]+`)
	windowsPathRE            = regexp.MustCompile(`(?i)` +
		`\b[A-Z]:[\\/][^\s"'<>|]+` +
		`|\\\\\?\\[A-Z]:[\\/][^\s"'<>|]+` +
		`|\\\\\?\\UNC[\\/][^\s"'<>|\\/]+[\\/][^\s"'<>|\\/]+[\\/][^\s"'<>|]+` +
		`|\\\\\.\\[^\s"'<>|\\/]+[\\/][^\s"'<>|]+` +
		`|\\\\[^\s"'<>|\\/]+[\\/][^\s"'<>|]+`)
	unixPathRE = regexp.MustCompile(`(^|[\s"'(=])(/[A-Za-z0-9._~@%+\-][^\s"'<>]*)`)
)

type Redactor struct {
	task       *Registry
	connection *Registry
	paths      *PathSanitizer
}

type Option func(*Redactor)

func WithTaskRegistry(registry *Registry) Option {
	return func(redactor *Redactor) {
		redactor.task = registry
	}
}

func WithConnectionRegistry(registry *Registry) Option {
	return func(redactor *Redactor) {
		redactor.connection = registry
	}
}

func WithPathSanitizer(sanitizer *PathSanitizer) Option {
	return func(redactor *Redactor) {
		redactor.paths = sanitizer
	}
}

func New(options ...Option) *Redactor {
	redactor := &Redactor{}
	for _, option := range options {
		option(redactor)
	}
	return redactor
}

func (r *Redactor) RedactString(text string) string {
	if text == "" {
		return text
	}

	if r != nil {
		text = redactSegmentsLongestFirst(text, r.SensitiveSegments())
		if r.paths != nil {
			text = r.paths.RedactString(text)
		} else {
			text = redactFreeformPaths(text)
		}
	} else {
		text = redactFreeformPaths(text)
	}
	return redactGenericSecrets(text)
}

func (r *Redactor) LongestSensitiveSegmentBytes() int {
	if r == nil {
		return 0
	}
	return max(r.task.LongestSegmentBytes(), r.connection.LongestSegmentBytes())
}

func (r *Redactor) SensitiveSegments() []string {
	if r == nil {
		return nil
	}
	segments := r.task.Segments()
	segments = append(segments, r.connection.Segments()...)
	return segments
}

func StructuredDrop(method string) bool {
	switch method {
	case "account/updated", "account/rateLimits/updated", "account/login/completed", "account/chatgptAuthTokens/refresh":
		return true
	default:
		return false
	}
}

func ContainsSecretLike(text string) bool {
	if text == "" {
		return false
	}
	if privateKeyBlockRE.MatchString(text) ||
		authorizationRE.MatchString(text) ||
		cookieHeaderRE.MatchString(text) ||
		sessionCookieRE.MatchString(text) ||
		jwtRE.MatchString(text) ||
		openAIKeyRE.MatchString(text) ||
		jsonSecretFieldRE.MatchString(text) ||
		containsSecretURL(text) {
		return true
	}
	if containsSensitiveJSONScalars(text) {
		return true
	}

	for _, match := range jsonGenericSecretFieldRE.FindAllStringSubmatch(text, -1) {
		if len(match) < 3 {
			continue
		}
		if jsonScalarTextIsMeaningful(match[2]) {
			return true
		}
	}

	for _, match := range genericAssignRE.FindAllStringSubmatch(text, -1) {
		if len(match) < 3 {
			continue
		}
		if len(strings.Trim(match[2], ` "'`)) > 8 {
			return true
		}
	}
	return false
}

func redactGenericSecrets(text string) string {
	text = privateKeyBlockRE.ReplaceAllString(text, "[REDACTED:private-key]")
	text = authorizationRE.ReplaceAllString(text, "${1}[REDACTED:authorization]")
	text = cookieHeaderRE.ReplaceAllString(text, "${1}[REDACTED:cookie]")
	text = sessionCookieRE.ReplaceAllString(text, "${1}[REDACTED:cookie]")
	if redacted, ok := redactSensitiveJSONScalars(text); ok {
		text = redacted
	}
	text = jsonSecretFieldRE.ReplaceAllStringFunc(text, func(match string) string {
		return redactJSONSecretField(match, jsonSecretFieldRE)
	})
	text = jsonGenericSecretFieldRE.ReplaceAllStringFunc(text, func(match string) string {
		return redactJSONSecretField(match, jsonGenericSecretFieldRE)
	})
	text = genericAssignRE.ReplaceAllStringFunc(text, redactGenericAssignment)
	text = openAIKeyRE.ReplaceAllString(text, "[REDACTED:api-key]")
	text = jwtRE.ReplaceAllString(text, "[REDACTED:jwt]")
	text = redactSecretURLs(text)
	return text
}

func jsonScalarTextIsMeaningful(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, `"`) || strings.HasPrefix(raw, `\"`) {
		return len(strings.Trim(raw, "\\\" ")) > 8
	}
	return true
}

func redactJSONSecretField(match string, fieldRE *regexp.Regexp) string {
	parts := fieldRE.FindStringSubmatch(match)
	if len(parts) < 3 {
		return match
	}
	quote := `"`
	if strings.Contains(parts[1], `\"`) || strings.HasPrefix(strings.TrimSpace(parts[2]), `\"`) {
		quote = `\"`
	}
	return parts[1] + quote + jsonSecretMarker + quote
}

func redactGenericAssignment(match string) string {
	parts := genericAssignRE.FindStringSubmatch(match)
	value := strings.TrimSpace(parts[2])
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return parts[1] + string(first) + "[REDACTED:secret]" + string(last)
		}
	}
	return parts[1] + "[REDACTED:secret]"
}

func redactFreeformPaths(text string) string {
	text = windowsPathRE.ReplaceAllString(text, PathMarker)
	return unixPathRE.ReplaceAllString(text, "${1}"+PathMarker)
}

func containsSecretURL(text string) bool {
	for _, candidate := range urlCandidateRE.FindAllString(text, -1) {
		if isSecretURL(candidate) {
			return true
		}
	}
	return false
}

func redactSecretURLs(text string) string {
	return urlCandidateRE.ReplaceAllStringFunc(text, func(candidate string) string {
		if isSecretURL(candidate) {
			return "[REDACTED:url]"
		}
		return candidate
	})
}

func isSecretURL(candidate string) bool {
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	return parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != ""
}
