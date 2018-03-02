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
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pires/nats-operator/pkg/constants"
	"github.com/pires/nats-operator/pkg/spec"
	kubernetesutil "github.com/pires/nats-operator/pkg/util/kubernetes"
	"github.com/pires/nats-operator/pkg/util/retryutil"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/net"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	monitoringPort = strconv.FormatInt(int64(constants.MonitoringPort), 10)
	retryInterval  = 10 * time.Second
)

type acceptFunc func(*spec.NatsCluster) bool
type filterFunc func(*v1.Pod) bool

type routez struct {
	RouteCount int `json:"num_routes"`
}

func WaitUntilPodSizeReached(t *testing.T, kubeClient corev1.CoreV1Interface, size, retries int, cl *spec.NatsCluster) ([]string, error) {
	var names []string
	err := retryutil.Retry(retryInterval, retries, func() (done bool, err error) {
		podList, err := kubeClient.Pods(cl.Namespace).List(kubernetesutil.ClusterListOpt(cl.Name))
		if err != nil {
			return false, err
		}
		names = nil
		var nodeNames []string
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Status.Phase != v1.PodRunning {
				continue
			}
			names = append(names, pod.Name)
			nodeNames = append(nodeNames, pod.Spec.NodeName)
		}
		LogfWithTimestamp(t, "waiting size (%d), NATS pods: names (%v), nodes (%v)", size, names, nodeNames)
		if len(names) != size {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func WaitUntilPodSizeAndRoutesReached(t *testing.T, kubeClient corev1.CoreV1Interface, size, retries int, cl *spec.NatsCluster) ([]string, error) {
	var names []string
	err := retryutil.Retry(retryInterval, retries, func() (done bool, err error) {
		podList, err := kubeClient.Pods(cl.Namespace).List(kubernetesutil.ClusterListOpt(cl.Name))
		if err != nil {
			return false, err
		}
		names = nil
		var nodeNames []string
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Status.Phase != v1.PodRunning {
				continue
			}

			// Check the "/routez" endpoint for the number of established routes.
			num, err := countRoutes(kubeClient, cl.Namespace, pod.Name)
			if err != nil {
				continue
			}
			if num != size-1 {
				LogfWithTimestamp(t, "pod(%v): expected # of routes(%v), current # of routes(%v)", pod.Name, size-1, num)
				continue
			}

			names = append(names, pod.Name)
			nodeNames = append(nodeNames, pod.Spec.NodeName)
		}
		LogfWithTimestamp(t, "waiting size (%d) with (%d) routes per pod, NATS pods: names (%v), nodes (%v)", size, size-1, names, nodeNames)
		if len(names) != size {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func WaitUntilPodSizeAndVersionAndRoutesReached(t *testing.T, kubeClient corev1.CoreV1Interface, version string, size, retries int, cl *spec.NatsCluster) error {
	return retryutil.Retry(retryInterval, retries, func() (done bool, err error) {
		var names []string
		podList, err := kubeClient.Pods(cl.Namespace).List(kubernetesutil.ClusterListOpt(cl.Name))
		if err != nil {
			return false, err
		}
		names = nil
		var nodeNames []string
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Status.Phase != v1.PodRunning {
				continue
			}

			containerVersion := getVersionFromImage(pod.Status.ContainerStatuses[0].Image)
			if containerVersion != version {
				LogfWithTimestamp(t, "pod(%v): expected version(%v) current version(%v)", pod.Name, version, containerVersion)
				continue
			}

			// Check the "/routez" endpoint for the number of established routes.
			num, err := countRoutes(kubeClient, cl.Namespace, pod.Name)
			if err != nil {
				continue
			}
			if num != size-1 {
				LogfWithTimestamp(t, "pod(%v): expected # of routes(%v), current # of routes(%v)", pod.Name, size-1, num)
				continue
			}

			names = append(names, pod.Name)
			nodeNames = append(nodeNames, pod.Spec.NodeName)
		}
		LogfWithTimestamp(t, "waiting size (%d), NATS pods: names (%v), nodes (%v)", size, names, nodeNames)
		if len(names) != size {
			return false, nil
		}
		return true, nil
	})
}

func getVersionFromImage(image string) string {
	return strings.Split(image, ":")[1]
}

func waitResourcesDeleted(t *testing.T, kubeClient corev1.CoreV1Interface, cl *spec.NatsCluster) error {
	undeletedPods, err := WaitPodsDeleted(kubeClient, cl.Namespace, 3, kubernetesutil.ClusterListOpt(cl.Name))
	if err != nil {
		if retryutil.IsRetryFailure(err) && len(undeletedPods) > 0 {
			p := undeletedPods[0]
			LogfWithTimestamp(t, "waiting pod (%s) to be deleted.", p.Name)

			buf := bytes.NewBuffer(nil)
			buf.WriteString("init container status:\n")
			printContainerStatus(buf, p.Status.InitContainerStatuses)
			buf.WriteString("container status:\n")
			printContainerStatus(buf, p.Status.ContainerStatuses)
			t.Logf("pod (%s) status.phase is (%s): %v", p.Name, p.Status.Phase, buf.String())
		}

		return fmt.Errorf("fail to wait pods deleted: %v", err)
	}

	err = retryutil.Retry(retryInterval, 3, func() (done bool, err error) {
		list, err := kubeClient.Services(cl.Namespace).List(kubernetesutil.ClusterListOpt(cl.Name))
		if err != nil {
			return false, err
		}
		if len(list.Items) > 0 {
			LogfWithTimestamp(t, "waiting service (%s) to be deleted", list.Items[0].Name)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("fail to wait services deleted: %v", err)
	}
	return nil
}

func WaitPodsDeleted(kubecli corev1.CoreV1Interface, namespace string, retries int, lo metav1.ListOptions) ([]*v1.Pod, error) {
	f := func(p *v1.Pod) bool { return p.DeletionTimestamp != nil }
	return waitPodsDeleted(kubecli, namespace, retries, lo, f)
}

func waitPodsDeleted(kubecli corev1.CoreV1Interface, namespace string, retries int, lo metav1.ListOptions, filters ...filterFunc) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	err := retryutil.Retry(retryInterval, retries, func() (bool, error) {
		podList, err := kubecli.Pods(namespace).List(lo)
		if err != nil {
			return false, err
		}
		pods = nil
		for i := range podList.Items {
			p := &podList.Items[i]
			filtered := false
			for _, filter := range filters {
				if filter(p) {
					filtered = true
				}
			}
			if !filtered {
				pods = append(pods, p)
			}
		}
		return len(pods) == 0, nil
	})
	return pods, err
}

// WaitUntilOperatorReady will wait until the first pod selected for the label name=nats-operator is ready.
func WaitUntilOperatorReady(kubecli corev1.CoreV1Interface, namespace, name string) error {
	var podName string
	lo := metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(NameLabelSelector(name)).String(),
	}
	err := retryutil.Retry(10*time.Second, 6, func() (bool, error) {
		podList, err := kubecli.Pods(namespace).List(lo)
		if err != nil {
			return false, err
		}
		if len(podList.Items) > 0 {
			podName = podList.Items[0].Name
			if kubernetesutil.IsPodReady(&podList.Items[0]) {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for pod (%v) to become ready: %v", podName, err)
	}
	return nil
}

func countRoutes(kubecli corev1.CoreV1Interface, namespace, name string) (int, error) {
	// Check the "/routez" endpoint for the number of established routes.
	b, err := kubecli.
		RESTClient().
		Get().
		Namespace(namespace).
		Resource("pods").
		SubResource("proxy").
		Name(net.JoinSchemeNamePort("http", name, monitoringPort)).
		Suffix("/routez").
		DoRaw()
	if err != nil {
		return 0, err
	}

	var (
		data routez
	)
	if err := json.Unmarshal(b, &data); err != nil {
		return 0, err
	}

	return data.RouteCount, nil
}
