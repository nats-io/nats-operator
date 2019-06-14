package versionCheck

import (
	"testing"
)

func TestServerBinaryPath(t *testing.T) {
	got := ServerBinaryPath("2.0.0")
	expected := NatsBinaryPath
	if got != expected {
		t.Fatalf("Expected %q, got: %q", expected, got)
	}

	got = ServerBinaryPath("2.1.8")
	expected = NatsBinaryPath
	if got != expected {
		t.Fatalf("Expected %q, got: %q", expected, got)
	}

	got = ServerBinaryPath("2.1.8-RC18")
	expected = NatsBinaryPath
	if got != expected {
		t.Fatalf("Expected %q, got: %q", expected, got)
	}

	got = ServerBinaryPath("0.1.8")
	expected = OldNatsBinaryPath
	if got != expected {
		t.Fatalf("Expected %q, got: %q", expected, got)
	}

	got = ServerBinaryPath("1.4.1")
	expected = OldNatsBinaryPath
	if got != expected {
		t.Fatalf("Expected %q, got: %q", expected, got)
	}
}
