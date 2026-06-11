package config

import (
	"fmt"
	"net"
	"strconv"
)

func ValidateListenAddress(address string) error {
	if address == "" {
		return fmt.Errorf("listen address is required")
	}

	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("listen must be host:port: %w", err)
	}
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("listen host must be localhost, 127.0.0.1, or [::1]")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("listen port must be numeric: %w", err)
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("listen port out of range")
	}
	return nil
}
