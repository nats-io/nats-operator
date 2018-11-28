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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	watchapi "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/watch"
	"k8s.io/kubernetes/pkg/util/slice"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/conf"
	"github.com/nats-io/nats-operator/pkg/constants"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	"github.com/nats-io/nats-operator/pkg/util/retryutil"
)

// varz encapsulates a response from the "/varz" endpoint of the NATS management API.
type varz struct {
	Version string `json:"version"`
}

// routez encapsulates a response from the "/routez" endpoint of the NATS management API.
type routez struct {
	RouteCount int `json:"num_routes"`
}

// NatsClusterCustomizer represents a function that allows for customizing a NatsCluster resource before it is created.
type NatsClusterCustomizer func(natsCluster *natsv1alpha2.NatsCluster)

// CreateCluster creates a NatsCluster resource which name starts with the specified prefix, and using the specified size and version.
// Before actually creating the NatsCluster resource, it allows for the resource to be customized via the application of NatsClusterCustomizer functions.
func (f *Framework) CreateCluster(prefix string, size int, version string, fn ...NatsClusterCustomizer) (*natsv1alpha2.NatsCluster, error) {
	// Create a NatsCluster object using the specified values.
	obj := &natsv1alpha2.NatsCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       natsv1alpha2.CRDResourceKind,
			APIVersion: natsv1alpha2.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Namespace:    f.Namespace,
		},
		Spec: natsv1alpha2.ClusterSpec{
			Paused:  false,
			Size:    size,
			Version: version,
		},
	}
	// Allow for customizing the NatsCluster resource before creation.
	for _, f := range fn {
		f(obj)
	}
	// Create the NatsCluster resource.
	return f.NatsClient.NatsV1alpha2().NatsClusters(obj.Namespace).Create(obj)
}

// DeleteCluster deletes the specified NatsCluster resource.
func (f *Framework) DeleteCluster(natsCluster *natsv1alpha2.NatsCluster) error {
	return f.NatsClient.NatsV1alpha2().NatsClusters(natsCluster.Namespace).Delete(natsCluster.Name, &metav1.DeleteOptions{})
}

// NatsClusterHasExpectedVersion returns whether every pod in the specified NatsCluster is running the specified version of NATS.
func (f *Framework) NatsClusterHasExpectedVersion(natsCluster *natsv1alpha2.NatsCluster, expectedVersion string) (bool, error) {
	// List pods belonging to the specified NatsCluster resource.
	pods, err := f.PodsForNatsCluster(natsCluster)
	if err != nil {
		return false, err
	}
	// Make sure that every pod is running the expected version of the NATS server.
	for _, pod := range pods {
		v, err := f.VersionForPod(pod)
		if err != nil {
			return false, err
		}
		if v != expectedVersion {
			return false, nil
		}
	}
	// All the members of the cluster are reporting the expected version.
	return true, nil
}

// NatsClusterHasExpectedRouteCount returns whether every pod in the specified NatsCluster is reporting the expected number of routes.
func (f *Framework) NatsClusterHasExpectedRouteCount(natsCluster *natsv1alpha2.NatsCluster, expectedSize int) (bool, error) {
	// List pods belonging to the specified NatsCluster resource.
	pods, err := f.PodsForNatsCluster(natsCluster)
	if err != nil {
		return false, err
	}
	// Make sure that every pod has routes to every other pod.
	for _, pod := range pods {
		r, err := f.RouteCountForPod(pod)
		if err != nil {
			return false, err
		}
		if r != expectedSize-1 {
			return false, nil
		}
	}
	// All the members of the cluster are reporting the expected route count.
	return true, nil
}

// PodsForNatsCluster returns a slice containing all pods that belong to the specified NatsCluster resource.
func (f *Framework) PodsForNatsCluster(natsCluster *natsv1alpha2.NatsCluster) ([]v1.Pod, error) {
	pods, err := f.KubeClient.CoreV1().Pods(natsCluster.Namespace).List(kubernetesutil.ClusterListOpt(natsCluster.Name))
	if err != nil {
		return nil, err
	}
	return pods.Items, nil
}

// RouteCountForPod returns the number of routes reported by the specified pod.
func (f *Framework) RouteCountForPod(pod v1.Pod) (int, error) {
	// Check the "/routez" endpoint for the number of established routes.
	r, err := http.Get(fmt.Sprintf("http://%s:%d/routez", pod.Status.PodIP, constants.MonitoringPort))
	if err != nil {
		return 0, err
	}
	defer r.Body.Close()
	// Deserialize the response and return the number of routes.
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return 0, err
	}
	data := &routez{}
	if err := json.Unmarshal(b, data); err != nil {
		return 0, err
	}
	return data.RouteCount, nil
}

// PatchCluster performs a patch on the specified NatsCluster resource to align its ".spec" field with the provided value.
// It takes the desired state as an argument and patches the NatsCluster resource accordingly.
func (f *Framework) PatchCluster(natsCluster *natsv1alpha2.NatsCluster) (*natsv1alpha2.NatsCluster, error) {
	// Grab the most up-to-date version of the provided NatsCluster resource.
	currentCluster, err := f.NatsClient.NatsV1alpha2().NatsClusters(natsCluster.Namespace).Get(natsCluster.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Create a deep copy of currentCluster so we can create a patch.
	newCluster := currentCluster.DeepCopy()
	// Make the spec of newCluster match the desired spec.
	newCluster.Spec = natsCluster.Spec
	// Patch the NatsCluster resource.
	bytes, err := kubernetesutil.CreatePatch(currentCluster, newCluster, natsv1alpha2.NatsCluster{})
	if err != nil {
		return nil, err
	}
	return f.NatsClient.NatsV1alpha2().NatsClusters(natsCluster.Namespace).Patch(natsCluster.Name, types.MergePatchType, bytes)
}

// VersionForPod returns the version of NATS reported by the specified pod.
func (f *Framework) VersionForPod(pod v1.Pod) (string, error) {
	// Check the "/varz" endpoint for the number of established routes.
	r, err := http.Get(fmt.Sprintf("http://%s:%d/varz", pod.Status.PodIP, constants.MonitoringPort))
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	// Deserialize the response and return the version.
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	data := &varz{}
	if err := json.Unmarshal(b, data); err != nil {
		return "", err
	}
	return data.Version, nil
}

// WaitUntilExpectedRoutesInConfig waits until the expected routes for the specified NatsCluster are present in its configuration secret.
func (f *Framework) WaitUntilExpectedRoutesInConfig(ctx context.Context, natsCluster *natsv1alpha2.NatsCluster) error {
	return f.WaitUntilSecretCondition(ctx, natsCluster, func(event watchapi.Event) (bool, error) {
		// Grab the secret from the event.
		secret := event.Object.(*v1.Secret)
		// Make sure that the "nats.conf" key is present in the secret.
		conf, ok := secret.Data[constants.ConfigFileName]
		if !ok {
			return false, nil
		}

		// Grab the ServerConfig object that corresponds to "nats.conf".
		confObj, err := natsconf.Unmarshal(conf)
		if err != nil {
			return false, nil
		}

		// List pods belonging to the NATS cluster, and make sure that every route is listed in the configuration.
		pods, err := f.PodsForNatsCluster(natsCluster)
		if err != nil {
			return false, nil
		}
		for _, pod := range pods {
			route := fmt.Sprintf("nats://%s.%s.%s.svc:%d", pod.Name, kubernetesutil.ManagementServiceName(natsCluster.Name), natsCluster.Namespace, constants.ClusterPort)
			if !slice.ContainsString(confObj.Cluster.Routes, route, nil) {
				return false, nil
			}
		}

		// We've found routes to all pods in the configuration!
		return true, nil
	})
}

// WaitUntilFullMeshWithVersion waits until all the pods belonging to the specified NatsCluster report the expected number of routes and the expected version.
func (f *Framework) WaitUntilFullMeshWithVersion(ctx context.Context, natsCluster *natsv1alpha2.NatsCluster, expectedSize int, expectedVersion string) error {
	// Wait for the expected size and version to be reported on the NatsCluster resource.
	err := f.WaitUntilNatsClusterCondition(ctx, natsCluster, func(event watchapi.Event) (bool, error) {
		nc := event.Object.(*natsv1alpha2.NatsCluster)
		return nc.Status.Size == expectedSize && nc.Status.CurrentVersion == expectedVersion, nil
	})
	if err != nil {
		return err
	}
	// Wait for all the pods to report the expected routes and version.
	return retryutil.RetryWithContext(ctx, 5*time.Second, func() (bool, error) {
		// Check whether the full mesh has formed with the expected size.
		m, err := f.NatsClusterHasExpectedRouteCount(natsCluster, expectedSize)
		if err != nil {
			return false, nil
		}
		if !m {
			return false, nil
		}
		// Check whether all pods in the cluster are reporting the expected version.
		v, err := f.NatsClusterHasExpectedVersion(natsCluster, expectedVersion)
		if err != nil {
			return false, nil
		}
		if !v {
			return false, nil
		}
		return true, nil
	})
}

// WaitUntilNatsClusterCondition waits until the specified condition is verified in the specified NatsCluster.
func (f *Framework) WaitUntilNatsClusterCondition(ctx context.Context, natsCluster *natsv1alpha2.NatsCluster, fn watch.ConditionFunc) error {
	// Create a field selector that matches the specified NatsCluster resource.
	fs := kubernetesutil.ByCoordinates(natsCluster.Namespace, natsCluster.Name)
	// Create a ListWatch so we can receive events for the matched pods.
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fs.String()
			return f.NatsClient.NatsV1alpha2().NatsClusters(natsCluster.Namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watchapi.Interface, error) {
			options.FieldSelector = fs.String()
			return f.NatsClient.NatsV1alpha2().NatsClusters(natsCluster.Namespace).Watch(options)
		},
	}
	// Watch for updates to the NatsCluster resource until fn is satisfied, or until the timeout is reached.
	last, err := watch.UntilWithSync(ctx, lw, &natsv1alpha2.NatsCluster{}, nil, fn)
	if err != nil {
		return err
	}
	if last == nil {
		return fmt.Errorf("no events received for natscluster %q", natsCluster.Name)
	}
	return nil
}

// WaitUntilPodLogLineMatches waits until a line in the logs for the pod with the specified index and belonging to the specified NatsCluster resource matches the provided regular expression.
func (f *Framework) WaitUntilPodLogLineMatches(ctx context.Context, natsCluster *natsv1alpha2.NatsCluster, podIndex int, regex string) error {
	req := f.KubeClient.CoreV1().Pods(natsCluster.Namespace).GetLogs(fmt.Sprintf("%s-%d", natsCluster.Name, podIndex), &v1.PodLogOptions{
		Container: "nats",
		Follow:    true,
	})
	r, err := req.Stream()
	if err != nil {
		return err
	}
	defer r.Close()
	rd := bufio.NewReader(r)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to find a matching log line within the specified timeout")
		default:
			// Read a single line from the logs and check whether it matches the specified regular expression.
			str, err := rd.ReadString('\n')
			if err != nil && err != io.EOF {
				return err
			}
			if err == io.EOF {
				return fmt.Errorf("failed to find a matching log line before EOF")
			}
			m, err := regexp.MatchString(regex, str)
			if err != nil {
				return err
			}
			// If the current log line matches the regular expression, return.
			if m {
				return nil
			}
		}
	}
}
