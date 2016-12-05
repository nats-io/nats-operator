// Copyright 2016 The nats-operator Authors
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

package k8sutil

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/pires/nats-operator/pkg/constants"
	"github.com/pires/nats-operator/pkg/spec"

	"k8s.io/kubernetes/pkg/api"
	apierrors "k8s.io/kubernetes/pkg/api/errors"
	unversionedAPI "k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/client/restclient"
	"k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/util/intstr"
	"k8s.io/kubernetes/pkg/util/wait"
	"k8s.io/kubernetes/pkg/watch"
)

const (
	versionAnnotationKey = "nats.version"
)

func GetNATSVersion(pod *api.Pod) string {
	return pod.Annotations[versionAnnotationKey]
}

func SetNATSVersion(pod *api.Pod, version string) {
	pod.Annotations[versionAnnotationKey] = version
}

func GetPodNames(pods []*api.Pod) []string {
	res := []string{}
	for _, p := range pods {
		res = append(res, p.Name)
	}
	return res
}

func MakeNATSImage(version string) string {
	return fmt.Sprintf("nats:%v", version)
}

func PodWithNodeSelector(p *api.Pod, ns map[string]string) *api.Pod {
	p.Spec.NodeSelector = ns
	return p
}

// CreateMgmtService creates an headless service for NATS management purposes.
func CreateMgmtService(kclient *unversioned.Client, clusterName, ns string) (*api.Service, error) {
	svc := makeMgmtServiceSpec(clusterName)
	retSvc, err := kclient.Services(ns).Create(svc)
	if err != nil {
		return nil, err
	}
	return retSvc, nil
}

// DeleteMgmtService deletes the headless service used for NATS management purposes.
func DeleteMgmtService(kclient *unversioned.Client, clusterName, ns string) error {
	svc := makeMgmtServiceSpec(clusterName)
	return kclient.Services(ns).Delete(svc.Name)
}

// CreateService creates an headless service for NATS clients to use.
func CreateService(kclient *unversioned.Client, clusterName, ns string) (*api.Service, error) {
	svc := makeServiceSpec(clusterName)
	retSvc, err := kclient.Services(ns).Create(svc)
	if err != nil {
		return nil, err
	}
	return retSvc, nil
}

// DeleteService deletes the headless service used ny NATS clients.
func DeleteService(kclient *unversioned.Client, clusterName, ns string) error {
	svc := makeServiceSpec(clusterName)
	return kclient.Services(ns).Delete(svc.Name)
}

func CreateAndWaitPod(kclient *unversioned.Client, ns string, pod *api.Pod, timeout time.Duration) error {
	if _, err := kclient.Pods(ns).Create(pod); err != nil {
		return err
	}
	w, err := kclient.Pods(ns).Watch(api.SingleObject(api.ObjectMeta{Name: pod.Name}))
	if err != nil {
		return err
	}
	_, err = watch.Until(timeout, w, unversioned.PodRunning)
	// TODO remove dead pod?
	//if err != nil {
	//	kclient.Pods(ns).Delete(pod.Name, &api.DeleteOptions{})
	//}

	return err
}

func makeServiceSpec(clusterName string) *api.Service {
	labels := map[string]string{
		"app":          "nats",
		"nats_cluster": clusterName,
	}
	svc := &api.Service{
		ObjectMeta: api.ObjectMeta{
			Name:   clusterName,
			Labels: labels,
		},
		Spec: api.ServiceSpec{
			ClusterIP: api.ClusterIPNone,
			Ports: []api.ServicePort{
				{
					Name:       "client",
					Port:       constants.ClientPort,
					TargetPort: intstr.FromInt(constants.ClientPort),
					Protocol:   api.ProtocolTCP,
				},
			},
			Selector: labels,
		},
	}
	return svc
}

func makeMgmtServiceSpec(clusterName string) *api.Service {
	labels := map[string]string{
		"app":          "nats-mgmt",
		"nats_cluster": clusterName,
	}
	svc := &api.Service{
		ObjectMeta: api.ObjectMeta{
			Name:   clusterName + "-mgmt",
			Labels: labels,
		},
		Spec: api.ServiceSpec{
			ClusterIP: api.ClusterIPNone,
			Ports: []api.ServicePort{
				{
					Name:       "cluster",
					Port:       constants.ClusterPort,
					TargetPort: intstr.FromInt(constants.ClusterPort),
					Protocol:   api.ProtocolTCP,
				},
				{
					Name:       "monitoring",
					Port:       constants.MonitoringPort,
					TargetPort: intstr.FromInt(constants.MonitoringPort),
					Protocol:   api.ProtocolTCP,
				},
			},
			Selector: labels,
		},
	}
	return svc
}

// MakePodSpec returns a NATS peer pod specification, based on the cluster specification.
func MakePodSpec(clusterName string, cs *spec.ClusterSpec) *api.Pod {
	// TODO add TLS, auth support, debug and tracing
	args := []string{
		fmt.Sprintf("--cluster=nats://0.0.0.0:%d", constants.ClusterPort),
		fmt.Sprintf("--http_port=%d", constants.MonitoringPort),
		fmt.Sprintf("--routes=nats://%s:%d", clusterName+"-mgmt", constants.ClusterPort),
	}

	pod := &api.Pod{
		ObjectMeta: api.ObjectMeta{
			GenerateName: clusterName + "-",
			Labels: map[string]string{
				"app":          "nats",
				"nats_cluster": clusterName,
			},
			Annotations: map[string]string{},
		},
		Spec: api.PodSpec{
			Containers: []api.Container{
				natsPodContainer(args, cs.Version),
			},
			RestartPolicy: api.RestartPolicyNever,
			// TODO use for TLS
			//Volumes: []api.Volume{
			//	{Name: "nats-tls", VolumeSource: api.VolumeSource{EmptyDir: &api.EmptyDirVolumeSource{}}},
			//},
		},
	}

	SetNATSVersion(pod, cs.Version)

	if cs.AntiAffinity {
		pod = PodWithAntiAffinity(pod, clusterName)
	}

	if len(cs.NodeSelector) != 0 {
		pod = PodWithNodeSelector(pod, cs.NodeSelector)
	}

	return pod
}

func MustGetInClusterMasterHost() string {
	cfg, err := restclient.InClusterConfig()
	if err != nil {
		panic(err)
	}
	return cfg.Host
}

// tlsConfig isn't modified inside this function.
// The reason it's a pointer is that it's not necessary to have tlsconfig to create a client.
func MustCreateClient(host string, tlsInsecure bool, tlsConfig *restclient.TLSClientConfig) *unversioned.Client {
	if len(host) == 0 {
		c, err := unversioned.NewInCluster()
		if err != nil {
			panic(err)
		}
		return c
	}
	cfg := &restclient.Config{
		Host:  host,
		QPS:   100,
		Burst: 100,
	}
	hostUrl, err := url.Parse(host)
	if err != nil {
		panic(fmt.Sprintf("error parsing host url %s : %v", host, err))
	}
	if hostUrl.Scheme == "https" {
		cfg.TLSClientConfig = *tlsConfig
		cfg.Insecure = tlsInsecure
	}
	c, err := unversioned.New(cfg)
	if err != nil {
		panic(err)
	}
	return c
}

func IsKubernetesResourceAlreadyExistError(err error) bool {
	se, ok := err.(*apierrors.StatusError)
	if !ok {
		return false
	}
	if se.Status().Code == http.StatusConflict && se.Status().Reason == unversionedAPI.StatusReasonAlreadyExists {
		return true
	}
	return false
}

func IsKubernetesResourceNotFoundError(err error) bool {
	se, ok := err.(*apierrors.StatusError)
	if !ok {
		return false
	}
	if se.Status().Code == http.StatusNotFound && se.Status().Reason == unversionedAPI.StatusReasonNotFound {
		return true
	}
	return false
}

func ListClusters(host, ns string, httpClient *http.Client) (*http.Response, error) {
	return httpClient.Get(fmt.Sprintf("%s/apis/nats.io/v1/namespaces/%s/natsclusters",
		host, ns))
}

func WatchClusters(host, ns string, httpClient *http.Client, resourceVersion string) (*http.Response, error) {
	return httpClient.Get(fmt.Sprintf("%s/apis/nats.io/v1/namespaces/%s/natsclusters?watch=true&resourceVersion=%s",
		host, ns, resourceVersion))
}

func WaitTPRReady(httpClient *http.Client, interval, timeout time.Duration, host, ns string) error {
	return wait.Poll(interval, timeout, func() (bool, error) {
		resp, err := ListClusters(host, ns, httpClient)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
			return true, nil
		case http.StatusNotFound: // not set up yet. wait.
			return false, nil
		default:
			return false, fmt.Errorf("invalid status code: %v", resp.Status)
		}
	})
}

func PodListOpt(clusterName string) api.ListOptions {
	return api.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app":          "nats",
			"nats_cluster": clusterName,
		}),
	}
}
