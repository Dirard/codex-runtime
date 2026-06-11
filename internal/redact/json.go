package redact

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

var sensitiveNameFragments = []string{
	"token",
	"secret",
	"password",
	"passwd",
	"api_key",
	"apikey",
	"access_key",
	"refresh_token",
	"access_token",
	"session",
	"cookie",
	"authorization",
	"credential",
	"client_secret",
}

func RegisterSensitiveJSONScalars(registry *Registry, raw json.RawMessage) error {
	if registry == nil || len(raw) == 0 {
		return nil
	}

	value, err := decodeJSONDocument(raw)
	if err != nil {
		return err
	}
	registerSensitiveJSONValue(registry, nil, value)
	return nil
}

func IsSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, fragment := range sensitiveNameFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func registerSensitiveJSONValue(registry *Registry, path []string, value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			registerSensitiveJSONValue(registry, append(path, key), child)
		}
	case []any:
		for _, child := range typed {
			registerSensitiveJSONValue(registry, path, child)
		}
	case string:
		registerSensitiveJSONScalar(registry, path, typed)
	case json.Number:
		registerSensitiveJSONScalar(registry, path, typed.String())
	case bool:
		registerSensitiveJSONScalar(registry, path, strconv.FormatBool(typed))
	}
}

func decodeJSONDocument(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

func registerSensitiveJSONScalar(registry *Registry, path []string, value string) {
	if hasSensitiveJSONPath(path) {
		registry.Add(value)
	}
}

func hasSensitiveJSONPath(path []string) bool {
	for _, segment := range path {
		if IsSensitiveName(segment) {
			return true
		}
	}
	return false
}

func containsSensitiveJSONScalars(text string) bool {
	value, err := decodeJSONDocument([]byte(text))
	if err != nil {
		return false
	}
	return containsSensitiveJSONValue(nil, value)
}

func containsSensitiveJSONValue(path []string, value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if containsSensitiveJSONValue(append(path, key), child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveJSONValue(path, child) {
				return true
			}
		}
	default:
		return hasSensitiveJSONPath(path) && sensitiveJSONScalarIsMeaningful(value)
	}
	return false
}

func redactSensitiveJSONScalars(text string) (string, bool) {
	value, err := decodeJSONDocument([]byte(text))
	if err != nil {
		return text, false
	}

	redacted, changed := redactSensitiveJSONValue(nil, value)
	if !changed {
		return text, false
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return text, false
	}
	return string(encoded), true
}

func redactSensitiveJSONValue(path []string, value any) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for key, child := range typed {
			redacted, childChanged := redactSensitiveJSONValue(append(path, key), child)
			if childChanged {
				typed[key] = redacted
				changed = true
			}
		}
		return typed, changed
	case []any:
		changed := false
		for index, child := range typed {
			redacted, childChanged := redactSensitiveJSONValue(path, child)
			if childChanged {
				typed[index] = redacted
				changed = true
			}
		}
		return typed, changed
	default:
		if hasSensitiveJSONPath(path) && jsonScalarIsRedactable(value) {
			return "[REDACTED:secret]", true
		}
		return value, false
	}
}

func sensitiveJSONScalarIsMeaningful(value any) bool {
	switch typed := value.(type) {
	case string:
		return len(strings.TrimSpace(typed)) > 8
	case json.Number:
		return strings.TrimSpace(typed.String()) != ""
	case bool:
		return true
	default:
		return false
	}
}

func jsonScalarIsRedactable(value any) bool {
	switch value.(type) {
	case string, json.Number, bool:
		return true
	default:
		return false
	}
}
