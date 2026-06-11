package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateAndLoadToken(source TokenSource) (string, error) {
	hasEnv := source.Env != ""
	hasFile := source.File != ""
	if hasEnv == hasFile {
		return "", fmt.Errorf("client_auth_token_source must set exactly one of env or file")
	}

	var value string
	if hasEnv {
		if err := validateEnvName(source.Env); err != nil {
			return "", fmt.Errorf("client_auth_token_source env: %w", err)
		}
		envValue, ok := os.LookupEnv(source.Env)
		if !ok {
			return "", fmt.Errorf("client_auth_token_source env is missing")
		}
		value = envValue
	} else {
		if !filepath.IsAbs(source.File) {
			return "", fmt.Errorf("client_auth_token_source file must be absolute")
		}
		data, err := os.ReadFile(source.File)
		if err != nil {
			return "", fmt.Errorf("read client_auth_token_source file: %w", err)
		}
		value = string(data)
		if strings.HasSuffix(value, "\r\n") {
			value = strings.TrimSuffix(value, "\r\n")
		} else if strings.HasSuffix(value, "\n") {
			value = strings.TrimSuffix(value, "\n")
		}
	}

	if err := validateTokenValue(value); err != nil {
		return "", fmt.Errorf("client auth token is invalid: %w", err)
	}
	return value, nil
}

func validateTokenValue(value string) error {
	if value == "" {
		return fmt.Errorf("empty")
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("leading or trailing whitespace")
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return fmt.Errorf("contains non-printable or non-ASCII character")
		}
	}
	if !isBearerTokenSyntax(value) {
		return fmt.Errorf("must use RFC 6750 bearer token characters")
	}
	return nil
}

func isBearerTokenSyntax(value string) bool {
	sawTokenChar := false
	sawPadding := false
	for _, char := range value {
		if char == '=' {
			if !sawTokenChar {
				return false
			}
			sawPadding = true
			continue
		}
		if sawPadding {
			return false
		}
		if !isBearerTokenChar(char) {
			return false
		}
		sawTokenChar = true
	}
	return sawTokenChar
}

func isBearerTokenChar(char rune) bool {
	return char >= 'A' && char <= 'Z' ||
		char >= 'a' && char <= 'z' ||
		char >= '0' && char <= '9' ||
		char == '-' ||
		char == '.' ||
		char == '_' ||
		char == '~' ||
		char == '+' ||
		char == '/'
}
