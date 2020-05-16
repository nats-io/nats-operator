package versionCheck

import (
	"strconv"
	"strings"
)

const (
	// OldNatsBinaryPath is the path to the NATS binary inside the
	// main container, before NATS Server v2.
	OldNatsBinaryPath = "/gnatsd"

	// NatsBinaryPath after v2 release.
	NatsBinaryPath = "/nats-server"
)

func ServerBinaryPath(version string) string {
	v := strings.Split(version, ".")
	if len(v) > 0 {
		majorVersion, err := strconv.Atoi(v[0])
		if err != nil {
			return NatsBinaryPath
		}
		if majorVersion < 2 {
			return OldNatsBinaryPath
		}
	}
	return NatsBinaryPath
}
