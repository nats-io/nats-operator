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
	"bufio"
	"fmt"
	"io"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	natsclient "github.com/nats-io/nats-operator/pkg/client/clientset/versioned"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

const (
	// natsOperatorPodName is the name of the nats-operator pod.
	natsOperatorPodName = "nats-operator"
	// natsOperatorE2ePodName is the name of the nats-operator-e2e pod.
	natsOperatorE2ePodName = "nats-operator-e2e"
)

// Framework encapsulates the configuration for the current run, and provides helper methods to be used during testing.
type Framework struct {
	// KubeClient is an interface to the Kubernetes base APIs.
	KubeClient kubernetes.Interface
	// Namespace is the namespace in which we are running.
	Namespace string
	// NatsClient is an interface to the nats.io/v1alpha2 API.
	NatsClient natsclient.Interface
}

// New returns a new instance of the testing framework.
func New(kubeconfig, namespace string) *Framework {
	config := kubernetesutil.MustNewKubeConfig(kubeconfig)
	kubeClient := kubernetesutil.MustNewKubeClientFromConfig(config)
	natsClient := kubernetesutil.MustNewNatsClientFromConfig(config)
	return &Framework{
		KubeClient: kubeClient,
		Namespace:  namespace,
		NatsClient: natsClient,
	}
}

// Cleanup deletes the nats-operator and nats-operator-e2e pods, ignoring errors.
func (f *Framework) Cleanup() {
	for _, pod := range []string{natsOperatorPodName, natsOperatorE2ePodName} {
		f.KubeClient.CoreV1().Pods(f.Namespace).Delete(pod, &metav1.DeleteOptions{})
	}
}

// WaitForNatsOperator waits for the nats-operator pod to be running and ready.
func (f *Framework) WaitForNatsOperator() error {
	// Create a "fake" pod object containing the expected namespace and name, as WaitUntilPodReady expects a pod instance.
	return kubernetesutil.WaitUntilPodReady(f.KubeClient.CoreV1(), &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: f.Namespace,
			Name:      natsOperatorPodName,
		},
	})
}

// WaitForNatsOperatorE2ePodTermination waits for the nats-operator pod to be running and ready.
// It then starts streaming logs and returns the pod's exit code, or an error if any error was found during the process.
func (f *Framework) WaitForNatsOperatorE2ePodTermination() (int, error) {
	// Create a "fake" pod object containing the expected namespace and name, as WaitUntilPodReady expects a pod instance.
	err := kubernetesutil.WaitUntilPodReady(f.KubeClient.CoreV1(), &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: f.Namespace,
			Name:      natsOperatorPodName,
		},
	})
	if err != nil {
		return -1, err
	}

	// Start streaming logs for the nats-operator-e2e until we receive io.EOF.
	req := f.KubeClient.CoreV1().Pods(f.Namespace).GetLogs(natsOperatorE2ePodName, &v1.PodLogOptions{
		Follow: true,
	})
	r, err := req.Stream()
	if err != nil {
		return -1, err
	}
	defer r.Close()
	b := bufio.NewReader(r)
	for {
		// Read a single line from the logs, and output it.
		l, err := b.ReadString('\n')
		if err != nil && err != io.EOF {
			return -1, err
		}
		if err == io.EOF {
			break
		}
		fmt.Print(l)
	}

	// Grab the first (and single) container's exit code so we can use it as our own exit code.
	pod, err := f.KubeClient.CoreV1().Pods(f.Namespace).Get(natsOperatorE2ePodName, metav1.GetOptions{})
	if err != nil {
		return -1, err
	}
	return int(pod.Status.ContainerStatuses[0].State.Terminated.ExitCode), nil
}
