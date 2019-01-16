package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/api/core/v1"
)

// TestTestIsNatsOperatorContainer tests the "IsNatsOperatorContainer" function.
func TestIsNatsOperatorContainer(t *testing.T) {
	tests := []struct {
		description    string
		container      v1.Container
		expectedResult bool
	}{
		{
			description: "first argument is \"nats-operator\"",
			container: v1.Container{
				Name:  "custom-name",
				Image: "custom-image:foo",
				Args: []string{
					"nats-operator",
				},
			},
			expectedResult: true,
		},
		{
			description: "first argument is \"/nats-operator\"",
			container: v1.Container{
				Name:  "custom-name",
				Image: "custom-image:foo",
				Args: []string{
					"/nats-operator",
				},
			},
			expectedResult: true,
		},
		{
			description: "container name is \"nats-operator\"",
			container: v1.Container{
				Name:  "nats-operator",
				Image: "custom-image:foo",
				Args: []string{
					"/custom-binary",
				},
			},
			expectedResult: true,
		},
		{
			description: "container image contains \"nats-operator\"",
			container: v1.Container{
				Name:  "custom-name",
				Image: "nats-operator:foo",
				Args: []string{
					"/custom-binary",
				},
			},
			expectedResult: true,
		},
		{
			description: "no args and custom name/image",
			container: v1.Container{
				Name:  "custom-name",
				Image: "custom-image:foo",
			},
			expectedResult: false,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.expectedResult, isNatsOperatorContainer(test.container), "test case: %s", test.description)
	}
}

// TestClusterScopedFeatureGateIsEnabled tests the "ClusterScopedFeatureGateIsEnabled" function.
func TestClusterScopedFeatureGateIsEnabled(t *testing.T) {
	tests := []struct {
		description    string
		args           []string
		expectedResult bool
	}{
		{
			description: "--feature-gates is not present",
			args: []string{
				"nats-operator",
				"--foo",
				"--bar",
			},
			expectedResult: false,
		},
		{
			description: "--feature-gates is present and has no value",
			args: []string{
				"nats-operator",
				"--foo",
				"--feature-gates",
			},
			expectedResult: false,
		},
		{
			description: "--feature-gates is present and ClusterScoped is enabled",
			args: []string{
				"nats-operator",
				"--foo",
				"--feature-gates=ClusterScoped=true",
			},
			expectedResult: true,
		},
		{
			description: "--feature-gates is present and ClusterScoped is disabled",
			args: []string{
				"nats-operator",
				"--foo",
				"--feature-gates=ClusterScoped=false",
			},
			expectedResult: false,
		},
		{
			description: "--feature-gates is present and ClusterScoped is enabled (separate positional arguments)",
			args: []string{
				"nats-operator",
				"--foo",
				"--feature-gates",
				"ClusterScoped=true",
			},
			expectedResult: true,
		},
		{
			description: "--feature-gates is present and ClusterScoped is disabled (separate positional arguments)",
			args: []string{
				"nats-operator",
				"--feature-gates",
				"Cluster-Scoped=false",
			},
			expectedResult: false,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.expectedResult, ClusterScopedFeatureGateIsEnabled(test.args), "test case: %s", test.description)
	}
}
