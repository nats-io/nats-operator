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
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	natsclient "github.com/nats-io/nats-operator/pkg/client/clientset/versioned"
	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/features"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

const (
	// natsOperatorDeploymentName is the name of the nats-operator deployment.
	natsOperatorDeploymentName = "nats-operator"

	// natsOperatorPodName is the name of the nats-operator pod.
	natsOperatorPodName = "nats-operator"

	// natsOperatorE2ePodName is the name of the nats-operator-e2e pod.
	natsOperatorE2ePodName = "nats-operator-e2e"

	// podReadinessTimeout is the maximum amount of time we wait
	// for the nats-operator / nats-operator-e2e pods to be
	// running and ready.
	podReadinessTimeout = 5 * time.Minute
)

// ClusterFeature represents a feature that can be enabled or disabled
// on the target Kubernetes cluster.
type ClusterFeature string

const (
	// TokenRequest represents the "TokenRequest" feature.
	TokenRequest = ClusterFeature("TokenRequest")

	// ShareProcessNamespace represents the "ShareProcessNamespace" feature.
	ShareProcessNamespace = ClusterFeature("ShareProcessNamespace")
)

// Framework encapsulates the configuration for the current run, and
// provides helper methods to be used during testing.
type Framework struct {
	// ClusterFeatures is a map indicating whether specific
	// cluster features have been detected in the target cluster.
	ClusterFeatures map[ClusterFeature]bool

	// FeatureMap is the map containing features and their status for the current instance of the end-to-end test suite.
	FeatureMap features.FeatureMap

	// KubeClient is an interface to the Kubernetes base APIs.
	KubeClient kubernetes.Interface

	// Namespace is the namespace in which we are running.
	Namespace string

	// NatsClient is an interface to the nats.io/v1alpha2 API.
	NatsClient natsclient.Interface
}

// New returns a new instance of the testing framework.
func New(featureMap features.FeatureMap, kubeconfig, namespace string) *Framework {
	// Assume that all features are disabled until we do feature detection.
	cf := map[ClusterFeature]bool{
		ShareProcessNamespace: false,
		TokenRequest:          false,
	}
	// Override the namespace if nats-operator is deployed in the cluster-scoped mode.
	if featureMap.IsEnabled(features.ClusterScoped) {
		namespace = constants.KubernetesNamespaceNatsIO
	}
	config := kubernetesutil.MustNewKubeConfig(kubeconfig)
	kubeClient := kubernetesutil.MustNewKubeClientFromConfig(config)
	natsClient := kubernetesutil.MustNewNatsClientFromConfig(config)
	return &Framework{
		ClusterFeatures: cf,
		FeatureMap:      featureMap,
		KubeClient:      kubeClient,
		Namespace:       namespace,
		NatsClient:      natsClient,
	}
}

// Cleanup deletes the nats-operator deployment and the nats-operator-e2e pod, ignoring errors.
func (f *Framework) Cleanup() {
	if err := f.KubeClient.CoreV1().Pods(f.Namespace).Delete(natsOperatorE2ePodName, &metav1.DeleteOptions{}); err != nil {
		log.Warnf("failed to delete the %q pod: %v", natsOperatorE2ePodName, err)
	}
}

// FeatureDetect performs feature detection on the target Kubernetes cluster.
func (f *Framework) FeatureDetect() {
	v, err := f.KubeClient.Discovery().ServerVersion()
	if err != nil {
		return
	}
	major, err := strconv.Atoi(strings.TrimSuffix(v.Major, "+"))
	if err != nil {
		return
	}
	minor, err := strconv.Atoi(strings.TrimSuffix(v.Minor, "+"))
	if err != nil {
		return
	}
	// The features we want to detect can only be expected in 1.12+ clusters.
	if major == 0 || major == 1 && minor < 12 {
		return
	}

	// Kubernetes 1.12 has support for PID namespace sharing
	// enabled by default, so no more detection is necessary.
	f.ClusterFeatures[ShareProcessNamespace] = true

	// Detect whether the TokenRequest API is active by performing
	// a GET request to the "/token" subresource of the "default"
	// service account.
	if _, err := f.KubeClient.CoreV1().RESTClient().Get().Resource("serviceaccounts").Namespace(f.Namespace).Name("default").SubResource("token").DoRaw(); err != nil {
		if errors.IsMethodNotSupported(err) {
			// We've got a "405 METHOD NOT ALLOWED" response instead of a "404 NOT FOUND".
			// This means that the "/token" subresource is
			// indeed enabled, and it is enough to
			// conclude that the feature is supported in
			// the current cluster.
			f.ClusterFeatures[TokenRequest] = true
		}
	}
}

// WaitForNatsOperator waits for the nats-operator deployment to have at least one available replica.
func (f *Framework) WaitForNatsOperator() error {
	// Create a "fake" pod object containing the expected
	// namespace and name, as WaitUntilPodReady expects a pod
	// instance.
	ctx, fn := context.WithTimeout(context.Background(), podReadinessTimeout)
	defer fn()
	return kubernetesutil.WaitUntilDeploymentCondition(ctx, f.KubeClient, f.Namespace, natsOperatorDeploymentName, func(event watch.Event) (bool, error) {
		switch event.Type {
		case watch.Error:
			return false, fmt.Errorf("got event of type error: %+v", event.Object)
		case watch.Deleted:
			return false, fmt.Errorf("deployment \"%s/%s\" has been deleted", f.Namespace, natsOperatorDeploymentName)
		default:
			deployment := event.Object.(*appsv1.Deployment)
			return deployment.Status.AvailableReplicas >= 1, nil
		}
	})
}

// WaitForNatsOperatorE2ePodTermination waits for the nats-operator
// pod to be running and ready.
// It then starts streaming logs and returns the pod's exit code, or
// an error if any error was found during the process.
func (f *Framework) WaitForNatsOperatorE2ePodTermination() (int, error) {
	// Create a "fake" pod object containing the expected
	// namespace and name, as WaitUntilPodReady expects a pod
	// instance.
	ctx, fn := context.WithTimeout(context.Background(), podReadinessTimeout)
	defer fn()
	err := kubernetesutil.WaitUntilPodReady(ctx, f.KubeClient.CoreV1(), &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: f.Namespace,
			Name:      natsOperatorE2ePodName,
		},
	})
	if err != nil {
		return -1, err
	}

	// Start streaming logs for the nats-operator-e2e until we
	// receive io.EOF.
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

	// Grab the first (and single) container's exit code so we can
	// use it as our own exit code.
	pod, err := f.KubeClient.CoreV1().Pods(f.Namespace).Get(natsOperatorE2ePodName, metav1.GetOptions{})
	if err != nil {
		return -1, err
	}
	return int(pod.Status.ContainerStatuses[0].State.Terminated.ExitCode), nil
}
