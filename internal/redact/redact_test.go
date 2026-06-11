package redact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericSecretClassRedactionBeforeTruncation(t *testing.T) {
	redactor := New()
	input := strings.Join([]string{
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz",
		"Set-Cookie: session=abcdef1234567890",
		"api_key=abcdefghijklmnopqrstuvwxyz",
		"sk-proj-abcdefghijklmnopqrstuvwxyz",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signaturepart12",
	}, "\n")

	output := redactor.RedactString(input)
	for _, secret := range []string{
		"abcdefghijklmnopqrstuvwxyz",
		"abcdef1234567890",
		"sk-proj-abcdefghijklmnopqrstuvwxyz",
		"eyJhbGciOiJIUzI1NiJ9",
	} {
		if strings.Contains(output, secret) {
			t.Fatalf("redacted output still contains secret fragment %q: %s", secret, output)
		}
	}
}

func TestPathSanitizerRejectsConfigMalformedAndSymlinkEscape(t *testing.T) {
	cwd := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configDir)
	sanitizer, err := NewPathSanitizer(cwd)
	if err != nil {
		t.Fatal(err)
	}

	if got := sanitizer.SanitizeLabel(filepath.Join(configDir, "codex", "config.toml")); got != PathMarker {
		t.Fatalf("config path label = %q, want %q", got, PathMarker)
	}
	if got := sanitizer.SanitizeLabel("bad\x00path"); got != PathMarker {
		t.Fatalf("malformed path label = %q, want %q", got, PathMarker)
	}
	for _, label := range []string{
		"~/.codex/config.toml",
		"file:///tmp/secret",
		"https://host/path",
		"C:foo",
		`\\server\share\secret.txt`,
		`\\?\C:\Users\me\secret.txt`,
		`\\.\C:\Users\me\secret.txt`,
		`\\??\C:\Users\me\secret.txt`,
		"src/file?token=abc",
		"src/file#fragment",
		"user:pass@host/path",
		"user@example.com/path",
	} {
		t.Run(label, func(t *testing.T) {
			if got := sanitizer.SanitizeLabel(label); got != PathMarker {
				t.Fatalf("unsafe label = %q, got %q, want %q", label, got, PathMarker)
			}
		})
	}

	outsideDir := t.TempDir()
	link := filepath.Join(cwd, "link")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Logf("skipping symlink escape assertion: %v", err)
		return
	}
	if got := sanitizer.SanitizeLabel(filepath.Join("link", "secret.txt")); got != PathMarker {
		t.Fatalf("symlink escape label = %q, want %q", got, PathMarker)
	}
}

func TestGenericSecretAssignmentRedactsWhitespaceValues(t *testing.T) {
	redactor := New()
	tests := []struct {
		name      string
		input     string
		expected  string
		fragments []string
	}{
		{
			name:      "quoted passphrase assignment",
			input:     `PASSWORD="correct horse battery staple"`,
			expected:  `PASSWORD="[REDACTED:secret]"`,
			fragments: []string{"correct horse battery staple", "horse battery staple"},
		},
		{
			name:      "colon value with spaces",
			input:     "password: correct horse battery staple",
			expected:  "password: [REDACTED:secret]",
			fragments: []string{"correct horse battery staple", "horse battery staple"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !ContainsSecretLike(tt.input) {
				t.Fatalf("ContainsSecretLike(%q) = false, want true", tt.input)
			}

			output := redactor.RedactString(tt.input)
			if output != tt.expected {
				t.Fatalf("redacted output = %q, want %q", output, tt.expected)
			}
			assertNoSensitiveFragments(t, output, tt.fragments...)
		})
	}
}

func TestGenericJSONSecretFieldDetectionAndRedaction(t *testing.T) {
	redactor := New()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "api key",
			input:    `{"api_key":"abcdefghijklmnopqrstuvwxyz"}`,
			expected: `{"api_key":"[REDACTED:secret]"}`,
		},
		{
			name:     "token",
			input:    `{"token":"abcdefghijklmnopqrstuvwxyz"}`,
			expected: `{"token":"[REDACTED:secret]"}`,
		},
		{
			name:     "nested api key",
			input:    `{"outer":{"api_key":"abcdefghijklmnopqrstuvwxyz"}}`,
			expected: `{"outer":{"api_key":"[REDACTED:secret]"}}`,
		},
		{
			name:     "numeric api key",
			input:    `{"api_key":123456789012}`,
			expected: `{"api_key":"[REDACTED:secret]"}`,
		},
		{
			name:     "bool token",
			input:    `{"token":true}`,
			expected: `{"token":"[REDACTED:secret]"}`,
		},
		{
			name:     "array under token",
			input:    `{"token":[123456789012,true,null]}`,
			expected: `{"token":["[REDACTED:secret]","[REDACTED:secret]",null]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !ContainsSecretLike(tt.input) {
				t.Fatalf("ContainsSecretLike(%q) = false, want true", tt.input)
			}

			output := redactor.RedactString(tt.input)
			if output != tt.expected {
				t.Fatalf("redacted output = %q, want %q", output, tt.expected)
			}
			assertNoSensitiveFragments(t, output, "abcdefghijklmnopqrstuvwxyz")
		})
	}
}

func TestGenericJSONSecretFieldLeavesNullUnredacted(t *testing.T) {
	redactor := New()
	input := `{"token":null}`

	if ContainsSecretLike(input) {
		t.Fatalf("ContainsSecretLike(%q) = true, want false", input)
	}
	if output := redactor.RedactString(input); output != input {
		t.Fatalf("redacted output = %q, want %q", output, input)
	}
}

func TestRegisterSensitiveJSONScalarsRegistersNonStringScalars(t *testing.T) {
	registry := NewRegistry()
	raw := json.RawMessage(`{"api_key":123456789012,"token":true,"session":false,"secret":null,"safe":987654321}`)
	if err := RegisterSensitiveJSONScalars(registry, raw); err != nil {
		t.Fatal(err)
	}

	output := New(WithTaskRegistry(registry)).RedactString("echo 123456789012 true false 987654321 null")

	assertNoSensitiveFragments(t, output, "123456789012", "true", "false")
	for _, fragment := range []string{"987654321", "null"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("redacted output removed non-sensitive fragment %q: %q", fragment, output)
		}
	}
}

func TestSecretURLDetectionAndRedactionCoversNonHTTPSchemes(t *testing.T) {
	redactor := New()
	tests := []struct {
		name      string
		input     string
		fragments []string
	}{
		{
			name:      "postgres userinfo",
			input:     "dsn=postgres://user:pass@db/app",
			fragments: []string{"user:pass", "db/app"},
		},
		{
			name:      "ssh userinfo",
			input:     "remote ssh://user:pass@host",
			fragments: []string{"user:pass", "host"},
		},
		{
			name:      "s3 signed query",
			input:     "artifact=s3://bucket/key?X-Amz-Signature=abc123456789",
			fragments: []string{"bucket/key", "X-Amz-Signature", "abc123456789"},
		},
		{
			name:      "custom fragment",
			input:     "callback=custom://host/path#private-fragment",
			fragments: []string{"host/path", "private-fragment"},
		},
		{
			name:      "forced query",
			input:     "download=s3://bucket/key?",
			fragments: []string{"bucket/key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !ContainsSecretLike(tt.input) {
				t.Fatalf("ContainsSecretLike(%q) = false, want true", tt.input)
			}

			output := redactor.RedactString(tt.input)
			if !strings.Contains(output, "[REDACTED:url]") {
				t.Fatalf("redacted output missing URL marker: %q", output)
			}
			assertNoSensitiveFragments(t, output, tt.fragments...)
		})
	}
}

func TestSensitiveRegistryPartitionsLongValues(t *testing.T) {
	value := strings.Repeat("a", maxSensitiveSegmentBytes) + strings.Repeat("b", maxSensitiveSegmentBytes) + "cccc"
	registry := NewRegistry()

	registry.Add(value)

	segments := registry.Segments()
	if len(segments) != 3 {
		t.Fatalf("segment count = %d, want 3", len(segments))
	}
	for _, segment := range segments {
		if len(segment) < minSensitiveValueBytes || len(segment) > maxSensitiveSegmentBytes {
			t.Fatalf("segment length = %d, want within [%d, %d]", len(segment), minSensitiveValueBytes, maxSensitiveSegmentBytes)
		}
	}
	output := registry.Redact("prefix " + value + " suffix")
	if strings.Contains(output, value) || strings.Contains(output, strings.Repeat("a", 16)) || strings.Contains(output, strings.Repeat("b", 16)) {
		t.Fatalf("long sensitive value was not fully redacted")
	}
}

func TestSensitiveRegistryRedactsPrefixOverlapsLongestFirst(t *testing.T) {
	shorter := "shared-sensitive"
	longer := shorter + "-suffix"
	registry := NewRegistry()
	registry.Add(shorter)
	registry.Add(longer)

	output := registry.Redact("value " + longer + " done")

	assertNoSensitiveFragments(t, output, shorter, longer, "-suffix")
}

func TestRedactorRedactsCrossRegistryPrefixOverlapsLongestFirst(t *testing.T) {
	shorter := "task-sensitive"
	longer := shorter + "-connection-suffix"
	task := NewRegistry()
	connection := NewRegistry()
	task.Add(shorter)
	connection.Add(longer)
	redactor := New(WithTaskRegistry(task), WithConnectionRegistry(connection))

	output := redactor.RedactString("value " + longer + " done")

	assertNoSensitiveFragments(t, output, shorter, longer, "-connection-suffix")
}

func TestStreamingRedactorCarriesSensitiveValuesAcrossChunks(t *testing.T) {
	registry := NewRegistry()
	registry.Add("split-secret-value")
	stream := NewStream(New(WithTaskRegistry(registry)))

	first := stream.Write("before split-sec")
	second := stream.Write("ret-value after")
	flushed := stream.Flush()
	output := first + second + flushed

	if strings.Contains(output, "split-secret-value") {
		t.Fatalf("stream output leaked sensitive value: %q", output)
	}
	if !strings.Contains(output, SensitiveValueMarker) {
		t.Fatalf("stream output missing sensitive marker: %q", output)
	}
}

func TestStreamingRedactorRedactsCrossRegistryPrefixOverlapsLongestFirst(t *testing.T) {
	shorter := "stream-sensitive"
	longer := shorter + "-connection-suffix"
	task := NewRegistry()
	connection := NewRegistry()
	task.Add(shorter)
	connection.Add(longer)
	stream := NewStream(New(WithTaskRegistry(task), WithConnectionRegistry(connection)))

	first := stream.Write("before " + shorter[:len(shorter)-3])
	second := stream.Write(shorter[len(shorter)-3:] + "-connection")
	third := stream.Write("-suffix after")
	output := first + second + third + stream.Flush()

	assertNoSensitiveFragments(t, output, shorter, longer, "-connection-suffix")
	if !strings.Contains(output, SensitiveValueMarker) {
		t.Fatalf("stream output missing sensitive marker: %q", output)
	}
}

func TestStreamingRedactorKeepsCarryForNonPrefixOverlapsAcrossChunks(t *testing.T) {
	firstSecret := "abcdef"
	secondSecret := "defghij"
	registry := NewRegistry()
	registry.Add(firstSecret)
	registry.Add(secondSecret)
	stream := NewStream(New(WithTaskRegistry(registry)))

	output := stream.Write("before abc")
	output += stream.Write("defg")
	output += stream.Write("hij after")
	output += stream.Flush()

	assertNoSensitiveFragments(t, output, firstSecret, secondSecret, "defg", "fghij", "ghij")
	if count := strings.Count(output, SensitiveValueMarker); count < 2 {
		t.Fatalf("stream output has %d sensitive markers, want at least 2: %q", count, output)
	}
}

func TestStreamingRedactorKeepsCarryBoundedAcrossBoundarySegment(t *testing.T) {
	secret := strings.Repeat("q", maxSensitiveSegmentBytes)
	registry := NewRegistry()
	registry.Add(secret)
	stream := NewStream(New(WithTaskRegistry(registry)))

	prefix := "before "
	trailer := " trailing bytes"
	splitAt := maxSensitiveSegmentBytes - 48

	first := stream.Write(prefix + secret[:splitAt])
	if got := len(stream.carry); got > maxStreamingCarryBytes {
		t.Fatalf("carry after first write = %d, want <= %d", got, maxStreamingCarryBytes)
	}

	second := stream.Write(secret[splitAt:] + trailer)
	if got := len(stream.carry); got > maxStreamingCarryBytes {
		t.Fatalf("carry after second write = %d, want <= %d", got, maxStreamingCarryBytes)
	}

	output := first + second + stream.Flush()
	if strings.Contains(output, secret) {
		t.Fatal("stream output leaked the complete sensitive segment")
	}
	if strings.Contains(output, strings.Repeat("q", 128)) {
		t.Fatal("stream output leaked a long sensitive fragment")
	}
	if !strings.Contains(output, SensitiveValueMarker) {
		t.Fatalf("stream output missing sensitive marker: %q", output)
	}
}

func assertNoSensitiveFragments(t *testing.T, output string, fragments ...string) {
	t.Helper()

	for _, fragment := range fragments {
		if strings.Contains(output, fragment) {
			t.Fatalf("redacted output still contains sensitive fragment %q: %q", fragment, output)
		}
	}
}

func TestFreeformAbsolutePathRedaction(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		fragments []string
	}{
		{
			name:      "windows drive forward slashes",
			input:     "open C:/Users/private-user/AppData/secret.txt",
			fragments: []string{"C:/Users", "private-user", "AppData", "secret.txt"},
		},
		{
			name:      "windows drive backslashes",
			input:     `open C:\Users\private-user\AppData\secret.txt`,
			fragments: []string{`C:\Users`, "private-user", "AppData", "secret.txt"},
		},
		{
			name:      "windows unc",
			input:     `open \\fileserver\private-share\secret.txt`,
			fragments: []string{"fileserver", "private-share", "secret.txt"},
		},
		{
			name:      "unix absolute",
			input:     "open /home/private-user/project/secret.txt",
			fragments: []string{"/home", "private-user", "project", "secret.txt"},
		},
	}

	redactor := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := redactor.RedactString(tt.input)
			if !strings.Contains(output, PathMarker) {
				t.Fatalf("redacted output missing path marker: %q", output)
			}
			for _, fragment := range tt.fragments {
				if strings.Contains(output, fragment) {
					t.Fatalf("redacted output still contains private path fragment %q: %q", fragment, output)
				}
			}
		})
	}
}

func TestPathSanitizer(t *testing.T) {
	cwd := t.TempDir()
	inside := filepath.Join(cwd, "nested")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	sanitizer, err := NewPathSanitizer(cwd)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := sanitizer.Sanitize(filepath.Join(inside, "file.txt")), "nested/file.txt"; got != want {
		t.Fatalf("inside path label = %q, want %q", got, want)
	}
	if got := sanitizer.Sanitize(outside); got != PathMarker {
		t.Fatalf("outside path label = %q, want %q", got, PathMarker)
	}
	if got, want := sanitizer.SanitizeLabel(filepath.Join("nested", "file.txt")), "nested/file.txt"; got != want {
		t.Fatalf("inside relative path label = %q, want %q", got, want)
	}
	if got := sanitizer.SanitizeLabel(filepath.Join("..", "outside", "secret.txt")); got != PathMarker {
		t.Fatalf("outside relative path label = %q, want %q", got, PathMarker)
	}
	if got := sanitizer.SanitizeLabel(`..\outside\secret.txt`); got != PathMarker {
		t.Fatalf("outside windows relative path label = %q, want %q", got, PathMarker)
	}
	if got := sanitizer.SanitizeLabel(`C:\Users\me\secret.txt`); got != PathMarker {
		t.Fatalf("windows absolute path label = %q, want %q", got, PathMarker)
	}
}
