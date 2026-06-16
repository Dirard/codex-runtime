package main

import "testing"

func TestValidateLocalGatewayAddrAcceptsLoopback(t *testing.T) {
	for _, target := range []string{
		"127.0.0.1:5575",
		"localhost:5575",
		"[::1]:5575",
		"dns:///localhost:5575",
		"passthrough:///127.0.0.1:5575",
	} {
		t.Run(target, func(t *testing.T) {
			if err := validateLocalGatewayAddr(target); err != nil {
				t.Fatalf("validateLocalGatewayAddr returned error: %v", err)
			}
		})
	}
}

func TestValidateLocalGatewayAddrRejectsRemote(t *testing.T) {
	for _, target := range []string{
		"192.0.2.10:5575",
		"example.com:5575",
		"dns:///example.com:5575",
	} {
		t.Run(target, func(t *testing.T) {
			if err := validateLocalGatewayAddr(target); err == nil {
				t.Fatal("validateLocalGatewayAddr returned nil")
			}
		})
	}
}
