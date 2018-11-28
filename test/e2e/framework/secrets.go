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
	"context"
	"fmt"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	watchapi "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/watch"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

// CreateSecret creates a Secret resource containing the specified key and value.
func (f *Framework) CreateSecret(key string, val []byte) (*v1.Secret, error) {
	// Create a Secret object using the specified values.
	obj := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-nats-",
			Namespace:    f.Namespace,
		},
		Data: map[string][]byte{
			key: val,
		},
	}
	return f.KubeClient.CoreV1().Secrets(f.Namespace).Create(obj)
}

// DeleteSecret deletes the specified Secret resource.
func (f *Framework) DeleteSecret(secret *v1.Secret) error {
	return f.KubeClient.CoreV1().Secrets(f.Namespace).Delete(secret.Name, &metav1.DeleteOptions{})
}

// PatchSecret performs a patch on the specified Secret resource to align its ".data" field with the provided value.
// It takes the desired state as an argument and patches the Secret resource accordingly.
func (f *Framework) PatchSecret(secret *v1.Secret) (*v1.Secret, error) {
	// Grab the most up-to-date version of the provided Secret resource.
	currentSecret, err := f.KubeClient.CoreV1().Secrets(secret.Namespace).Get(secret.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Create a deep copy of currentSecret so we can create a patch.
	newSecret := currentSecret.DeepCopy()
	// Make the data of newSecret match the desired data.
	newSecret.Data = secret.Data
	// Patch the Secret resource.
	bytes, err := kubernetesutil.CreatePatch(currentSecret, newSecret, v1.Secret{})
	if err != nil {
		return nil, err
	}
	return f.KubeClient.CoreV1().Secrets(secret.Namespace).Patch(secret.Name, types.MergePatchType, bytes)
}

// WaitUntilSecretCondition waits until the specified condition is verified in configuration secret for the specified NatsCluster resource.
func (f *Framework) WaitUntilSecretCondition(ctx context.Context, natsCluster *natsv1alpha2.NatsCluster, fn watch.ConditionFunc) error {
	// Create a label selector that matches resources belonging to the specified NatsCluster resource.
	ls := labels.SelectorFromSet(kubernetesutil.LabelsForCluster(natsCluster.Name))
	// Create a ListWatch so we can receive events for the matched secrets.
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = ls.String()
			return f.KubeClient.CoreV1().Secrets(natsCluster.Namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watchapi.Interface, error) {
			options.LabelSelector = ls.String()
			return f.KubeClient.CoreV1().Secrets(natsCluster.Namespace).Watch(options)
		},
	}
	// Watch for updates to the matched secrets until fn is satisfied, or until the timeout is reached.
	last, err := watch.UntilWithSync(ctx, lw, &v1.Secret{}, nil, fn)
	if err != nil {
		return err
	}
	if last == nil {
		return fmt.Errorf("no events received for secrets belonging to natscluster %q", natsCluster.Name)
	}
	return nil
}
