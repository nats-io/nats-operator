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

package e2eutil

import (
	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewCluster(genName string, size int) *v1alpha2.NatsCluster {
	return &v1alpha2.NatsCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha2.CRDResourceKind,
			APIVersion: v1alpha2.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: genName,
		},
		Spec: v1alpha2.ClusterSpec{
			Size: size,
		},
	}
}

func ClusterWithSize(cl *v1alpha2.NatsCluster, size int) *v1alpha2.NatsCluster {
	cl.Spec.Size = size
	return cl
}

func ClusterWithVersion(cl *v1alpha2.NatsCluster, version string) *v1alpha2.NatsCluster {
	cl.Spec.Version = version
	return cl
}

// NameLabelSelector returns a label selector of the form name=<name>
func NameLabelSelector(name string) map[string]string {
	return map[string]string{"name": name}
}
