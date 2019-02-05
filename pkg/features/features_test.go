// Copyright 2017 The nats-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package features_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nats-io/nats-operator/pkg/features"
)

// TestParseFeatureMap tests the "ParseFeatureMap" function.
func TestParseFeatureMap(t *testing.T) {
	tests := []struct {
		description        string
		str                string
		expectedFeatureMap features.FeatureMap
		expectedError      error
	}{
		{
			description: "empty feature gate string",
			str:         "",
			expectedFeatureMap: features.FeatureMap{
				features.ClusterScoped: false,
			},
			expectedError: nil,
		},
		{
			description:        "malformed feature gate string",
			str:                "foobar",
			expectedFeatureMap: nil,
			expectedError:      fmt.Errorf("invalid key/value pair: %q", "foobar"),
		},
		{
			description: "valid feature gate string enabling a feature",
			str:         "ClusterScoped=true",
			expectedFeatureMap: features.FeatureMap{
				features.ClusterScoped: true,
			},
			expectedError: nil,
		},
		{
			description: "valid feature gate string disabling a feature",
			str:         "ClusterScoped=false",
			expectedFeatureMap: features.FeatureMap{
				features.ClusterScoped: false,
			},
			expectedError: nil,
		},
		{
			description: "feature gate string with \"empty\" key/value pair",
			str:         "ClusterScoped=true,",
			expectedFeatureMap: features.FeatureMap{
				features.ClusterScoped: true,
			},
			expectedError: nil,
		},
		{
			description:        "feature gate string with invalid key",
			str:                "UnknownFeature=true",
			expectedFeatureMap: nil,
			expectedError:      fmt.Errorf("invalid feature key: %q", "UnknownFeature"),
		},
		{
			description:        "feature gate string with invalid value",
			str:                "ClusterScoped=foo",
			expectedFeatureMap: nil,
			expectedError:      fmt.Errorf("failed to parse %q as a boolean value", "foo"),
		},
	}
	for _, test := range tests {
		m, err := features.ParseFeatureMap(test.str)
		assert.Equal(t, test.expectedFeatureMap, m, "test case: %s", test.description)
		assert.Equal(t, test.expectedError, err, "test case: %s", test.description)
	}
}

// TestIsEnabled tests the "IsEnabled" function.
func TestIsEnabled(t *testing.T) {
	var (
		m features.FeatureMap
	)
	m = features.FeatureMap{
		features.ClusterScoped: true,
	}
	assert.True(t, m.IsEnabled(features.ClusterScoped))
	m = features.FeatureMap{
		features.ClusterScoped: false,
	}
	assert.False(t, m.IsEnabled(features.ClusterScoped))
}
