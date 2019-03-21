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

package framework

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// namespaceNamePrefix is the prefix used when creating namespaces with a random name.
	namespaceNamePrefix = "nats-operator-e2e-"
)

// CreateNamespace creates a namespace with a random name.
func (f *Framework) CreateNamespace() (*corev1.Namespace, error) {
	return f.KubeClient.CoreV1().Namespaces().Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namespaceNamePrefix,
		},
	})
}

// DeleteNamespace deletes the specified namespace.
func (f *Framework) DeleteNamespace(namespace *corev1.Namespace) error {
	return f.KubeClient.CoreV1().Namespaces().Delete(namespace.Name, &metav1.DeleteOptions{})
}
