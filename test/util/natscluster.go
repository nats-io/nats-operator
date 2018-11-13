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

package util

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	watchapi "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/watch"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	v1alpha2client "github.com/nats-io/nats-operator/pkg/client/clientset/versioned/typed/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

// WaitForNatsClusterCondition establishes a watch on the specified NatsCluster resource and blocks until the specified condition is satisfied.
func WaitForNatsClusterCondition(natsClient *v1alpha2client.NatsV1alpha2Client, natsCluster *v1alpha2.NatsCluster, fn watch.ConditionFunc) error {
	// Create a selector that targets the specified NatsCluster resource.
	fs := kubernetes.ByCoordinates(natsCluster.Namespace, natsCluster.Name)
	// Grab a ListerWatcher with which we can watch the pod.
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fs.String()
			return natsClient.NatsClusters(natsCluster.Namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watchapi.Interface, error) {
			options.FieldSelector = fs.String()
			return natsClient.NatsClusters(natsCluster.Namespace).Watch(options)
		},
	}
	// Watch for updates to the specified NatsCluster resource until fn is satisfied.
	last, err := watch.UntilWithSync(context.TODO(), lw, &v1alpha2.NatsCluster{}, nil, fn)
	if err != nil {
		return err
	}
	if last == nil {
		return fmt.Errorf("no events received for natscluster %q", natsCluster.Name)
	}
	return nil
}
