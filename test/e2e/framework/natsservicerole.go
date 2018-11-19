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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

// NatsServiceRoleCustomizer represents a function that allows for customizing a NatsServiceRole resource before it is created.
type NatsServiceRoleCustomizer func(natsServiceRole *natsv1alpha2.NatsServiceRole)

// CreateNatsServiceRole creates a NatsServiceRole resource which name starts with the specified prefix.
// Before actually creating the CreateNatsServiceRole resource, it allows for the resource to be customized via the application of NatsServiceRoleCustomizer functions.
func (f *Framework) CreateNatsServiceRole(prefix string, fn ...NatsServiceRoleCustomizer) (*natsv1alpha2.NatsServiceRole, error) {
	// Create a ServiceAccount object to back the NatsServiceRole.
	// This is required for authentication to work as expected.
	a := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Namespace:    f.Namespace,
		},
	}
	a, err := f.KubeClient.CoreV1().ServiceAccounts(a.Namespace).Create(a)
	if err != nil {
		return nil, err
	}
	// Create a NatsServiceRole object using the specified values.
	obj := &natsv1alpha2.NatsServiceRole{
		ObjectMeta: metav1.ObjectMeta{
			Labels: make(map[string]string, 0),
			// Use the service account's name.
			Name:      a.Name,
			Namespace: a.Namespace,
		},
		Spec: natsv1alpha2.ServiceRoleSpec{
			Permissions: natsv1alpha2.Permissions{
				Publish:   []string{},
				Subscribe: []string{},
			},
		},
	}
	// Allow for customizing the NatsServiceRole resource before creation.
	for _, f := range fn {
		f(obj)
	}
	// Create the NatsServiceRole resource.
	return f.NatsClient.NatsV1alpha2().NatsServiceRoles(obj.Namespace).Create(obj)
}

// DeleteNatsServiceRole deletes the specified NatsServiceRole resource.
func (f *Framework) DeleteNatsServiceRole(nsr *natsv1alpha2.NatsServiceRole) error {
	// Delete the service account that backs the NatsServiceRole.
	// Avoid erroring if we fail to delete the service account, as it is not the primary intent of this function.
	_ = f.KubeClient.CoreV1().ServiceAccounts(f.Namespace).Delete(nsr.Name, &metav1.DeleteOptions{})
	// Delete the NatsServiceRole with the specified name.
	return f.NatsClient.NatsV1alpha2().NatsServiceRoles(f.Namespace).Delete(nsr.Name, &metav1.DeleteOptions{})
}

// PatchNatsServiceRole performs a patch on the specified NatsServiceRole resource to align its ".spec" field with the provided value.
// It takes the desired state as an argument and patches the NatsServiceRole resource accordingly.
func (f *Framework) PatchNatsServiceRole(nsr *natsv1alpha2.NatsServiceRole) (*natsv1alpha2.NatsServiceRole, error) {
	// Grab the most up-to-date version of the provided NatsServiceRole resource.
	currentNsr, err := f.NatsClient.NatsV1alpha2().NatsServiceRoles(nsr.Namespace).Get(nsr.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Create a deep copy of currentNsr so we can create a patch.
	newNss := currentNsr.DeepCopy()
	// Make the spec of newNss match the desired spec.
	newNss.Spec = nsr.Spec
	// Patch the NatsServiceRole resource.
	bytes, err := kubernetesutil.CreatePatch(currentNsr, newNss, natsv1alpha2.NatsServiceRole{})
	if err != nil {
		return nil, err
	}
	return f.NatsClient.NatsV1alpha2().NatsServiceRoles(nsr.Namespace).Patch(nsr.Name, types.MergePatchType, bytes)
}
