package config

import (
	"fmt"
	"os"
)

type loadOptions struct {
	listenOverride string
}

type LoadOption func(*loadOptions)

func WithListenOverride(address string) LoadOption {
	return func(options *loadOptions) {
		options.listenOverride = address
	}
}

func LoadFile(path string, options ...LoadOption) (*ValidatedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	raw, err := ParseTOML(data)
	if err != nil {
		return nil, err
	}

	resolvedOptions := loadOptions{}
	for _, option := range options {
		option(&resolvedOptions)
	}
	if resolvedOptions.listenOverride != "" {
		raw.Listen = resolvedOptions.listenOverride
	}

	return raw.Validate()
}
