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

package kubernetes

import (
	"fmt"

	"k8s.io/apimachinery/pkg/fields"
)

// ByCoordinates returns a field selector that can be used to filter Kubernetes resources based on their name and namespace.
func ByCoordinates(namespace, name string) fields.Selector {
	return fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name==%s,metadata.namespace==%s", name, namespace))
}
