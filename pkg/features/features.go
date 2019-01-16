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

package features

import (
	"fmt"
	"strconv"
	"strings"
)

// Feature represents a feature of nats-operator.
type Feature string

// FeatureMap is a mapping between features of nats-operator and their current status.
type FeatureMap map[Feature]bool

const (
	// ClusterScoped is used to indicate whether nats-operator should operate at the namespace or cluster level.
	ClusterScoped Feature = "ClusterScoped"
)

var (
	// defaultFeatureMap represents the default status of the nats-operator feature gates.
	defaultFeatureMap = map[Feature]bool{
		ClusterScoped: false,
	}
)

// ParseFeatureMap parses the specified string into a feature map.
func ParseFeatureMap(str string) (FeatureMap, error) {
	// Create the feature map we will be returning.
	res := make(FeatureMap, len(defaultFeatureMap))
	// Set all features to their default status.
	for feature, status := range defaultFeatureMap {
		res[feature] = status
	}
	// Split the provided string by "," in order to obtain all the "key=value" pairs.
	kvs := strings.Split(str, ",")
	// Iterate over all the "key=value" pairs and set the status of the corresponding feature in the feature map.
	for _, kv := range kvs {
		// Skip "empty" key/value pairs.
		if len(kv) == 0 {
			continue
		}
		// Split the key/value pair by "=".
		p := strings.Split(kv, "=")
		if len(p) != 2 {
			return nil, fmt.Errorf("invalid key/value pair: %q", kv)
		}
		// Grab the key and its value.
		k, v := p[0], p[1]
		// Make sure the feature corresponding to the key exists.
		if _, exists := defaultFeatureMap[Feature(k)]; !exists {
			return nil, fmt.Errorf("invalid feature key: %q", k)
		}
		// Attempt to parse the value as a boolean.
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %q as a boolean value", v)
		}
		// Set the feature's status in the feature map.
		res[Feature(k)] = b
	}
	// Return the feature map.
	return res, nil
}

// IsEnabled returns a value indicating whether the specified feature is enabled.
func (m FeatureMap) IsEnabled(feature Feature) bool {
	return m[feature]
}
