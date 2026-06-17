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

func TestShortID(t *testing.T) {
	if got := shortID(""); got != "-" {
		t.Fatalf("shortID(empty) = %q", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Fatalf("shortID(short) = %q", got)
	}
	if got := shortID("123456789012345"); got != "123456789012" {
		t.Fatalf("shortID(long) = %q", got)
	}
}
