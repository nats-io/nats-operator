package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/api/core/v1"
)

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

func TestExperimentalClusterScopedFlagIsSet(t *testing.T) {
	tests := []struct {
		description    string
		args           []string
		expectedResult bool
	}{
		{
			description: "--experimental-cluster-scoped is not present",
			args: []string{
				"nats-operator",
				"--foo",
				"--bar",
			},
			expectedResult: false,
		},
		{
			description: "--experimental-cluster-scoped is present and has no value",
			args: []string{
				"nats-operator",
				"--experimental-cluster-scoped",
				"--foo",
				"--bar",
			},
			expectedResult: true,
		},
		{
			description: "--experimental-cluster-scoped is present and equal to \"true\"",
			args: []string{
				"nats-operator",
				"--experimental-cluster-scoped=true",
				"--foo",
				"--bar",
			},
			expectedResult: true,
		},
		{
			description: "--experimental-cluster-scoped is present and equal to \"1\"",
			args: []string{
				"nats-operator",
				"--experimental-cluster-scoped=1",
				"--foo",
				"--bar",
			},
			expectedResult: true,
		},
		{
			description: "--experimental-cluster-scoped is present and equal to \"false\"",
			args: []string{
				"nats-operator",
				"--experimental-cluster-scoped=false",
				"--foo",
				"--bar",
			},
			expectedResult: false,
		},
		{
			description: "--experimental-cluster-scoped is present and equal to \"0\"",
			args: []string{
				"nats-operator",
				"--experimental-cluster-scoped=0",
				"--foo",
				"--bar",
			},
			expectedResult: false,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.expectedResult, experimentalClusterScopedFlagIsSet(test.args), "test case: %s", test.description)
	}
}
