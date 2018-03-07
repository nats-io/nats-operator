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
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-operator/pkg/client"
	"github.com/nats-io/nats-operator/pkg/spec"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	"github.com/nats-io/nats-operator/pkg/util/retryutil"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

func CreateCluster(t *testing.T, crClient client.NatsClusterCR, namespace string, cl *spec.NatsCluster) (*spec.NatsCluster, error) {
	cl.Namespace = namespace
	res, err := crClient.Create(context.TODO(), cl)
	if err != nil {
		return nil, err
	}
	t.Logf("creating NATS cluster: %s", res.Name)

	return res, nil
}

func UpdateCluster(crClient client.NatsClusterCR, cl *spec.NatsCluster, maxRetries int, updateFunc kubernetesutil.NatsClusterCRUpdateFunc) (*spec.NatsCluster, error) {
	return AtomicUpdateClusterCR(crClient, cl.Name, cl.Namespace, maxRetries, updateFunc)
}

func AtomicUpdateClusterCR(crClient client.NatsClusterCR, name, namespace string, maxRetries int, updateFunc kubernetesutil.NatsClusterCRUpdateFunc) (*spec.NatsCluster, error) {
	result := &spec.NatsCluster{}
	err := retryutil.Retry(1*time.Second, maxRetries, func() (done bool, err error) {
		natsCluster, err := crClient.Get(context.TODO(), namespace, name)
		if err != nil {
			return false, err
		}

		updateFunc(natsCluster)

		result, err = crClient.Update(context.TODO(), natsCluster)
		if err != nil {
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return result, err
}

func DeleteCluster(t *testing.T, crClient client.NatsClusterCR, kubeClient corev1.CoreV1Interface, cl *spec.NatsCluster) error {
	t.Logf("deleting NATS cluster: %v", cl.Name)
	err := crClient.Delete(context.TODO(), cl.Namespace, cl.Name)
	if err != nil {
		return err
	}
	return waitResourcesDeleted(t, kubeClient, cl)
}
