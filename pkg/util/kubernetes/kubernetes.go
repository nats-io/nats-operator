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

package kubernetes

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/pires/nats-operator/pkg/constants"
	"github.com/pires/nats-operator/pkg/spec"
	"github.com/pires/nats-operator/pkg/util/retryutil"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // for gcp auth
	"k8s.io/client-go/rest"
)

const (
	TolerateUnreadyEndpointsAnnotation = "service.alpha.kubernetes.io/tolerate-unready-endpoints"
	versionAnnotationKey               = "nats.version"
)

func GetNATSVersion(pod *v1.Pod) string {
	return pod.Annotations[versionAnnotationKey]
}

func SetNATSVersion(pod *v1.Pod, version string) {
	pod.Annotations[versionAnnotationKey] = version
}

func GetPodNames(pods []*v1.Pod) []string {
	if len(pods) == 0 {
		return nil
	}
	res := []string{}
	for _, p := range pods {
		res = append(res, p.Name)
	}
	return res
}

func MakeNATSImage(version string) string {
	return fmt.Sprintf("nats:%v", version)
}

func PodWithNodeSelector(p *v1.Pod, ns map[string]string) *v1.Pod {
	p.Spec.NodeSelector = ns
	return p
}

func createService(kubecli corev1client.CoreV1Interface, svcName, clusterName, ns, clusterIP string, ports []v1.ServicePort, owner metav1.OwnerReference) error {
	svc := newNatsServiceManifest(svcName, clusterName, clusterIP, ports)
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	_, err := kubecli.Services(ns).Create(svc)
	return err
}

func CreateClientService(kubecli corev1client.CoreV1Interface, clusterName, ns string, owner metav1.OwnerReference) error {
	ports := []v1.ServicePort{{
		Name:       "client",
		Port:       constants.ClientPort,
		TargetPort: intstr.FromInt(constants.ClientPort),
		Protocol:   v1.ProtocolTCP,
	}}
	return createService(kubecli, clusterName, clusterName, ns, "", ports, owner)
}

func ManagementServiceName(clusterName string) string {
	return clusterName + "-mgmt"
}

// CreateMgmtService creates an headless service for NATS management purposes.
func CreateMgmtService(kubecli corev1client.CoreV1Interface, clusterName, ns string, owner metav1.OwnerReference) error {
	ports := []v1.ServicePort{
		{
			Name:       "cluster",
			Port:       constants.ClusterPort,
			TargetPort: intstr.FromInt(constants.ClusterPort),
			Protocol:   v1.ProtocolTCP,
		},
		{
			Name:       "monitoring",
			Port:       constants.MonitoringPort,
			TargetPort: intstr.FromInt(constants.MonitoringPort),
			Protocol:   v1.ProtocolTCP,
		},
	}
	return createService(kubecli, ManagementServiceName(clusterName), clusterName, ns, v1.ClusterIPNone, ports, owner)
}

// CreateAndWaitPod is an util for testing.
// We should eventually get rid of this in critical code path and move it to test util.
func CreateAndWaitPod(kubecli corev1client.CoreV1Interface, ns string, pod *v1.Pod, timeout time.Duration) (*v1.Pod, error) {
	_, err := kubecli.Pods(ns).Create(pod)
	if err != nil {
		return nil, err
	}

	interval := 5 * time.Second
	var retPod *v1.Pod
	err = retryutil.Retry(interval, int(timeout/(interval)), func() (bool, error) {
		retPod, err = kubecli.Pods(ns).Get(pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		switch retPod.Status.Phase {
		case v1.PodRunning:
			return true, nil
		case v1.PodPending:
			return false, nil
		default:
			return false, fmt.Errorf("unexpected pod status.phase: %v", retPod.Status.Phase)
		}
	})

	if err != nil {
		if retryutil.IsRetryFailure(err) {
			return nil, fmt.Errorf("failed to wait pod running, it is still pending: %v", err)
		}
		return nil, fmt.Errorf("failed to wait pod running: %v", err)
	}

	return retPod, nil
}

func newNatsServiceManifest(svcName, clusterName, clusterIP string, ports []v1.ServicePort) *v1.Service {
	labels := map[string]string{
		"app":          "nats",
		"nats_cluster": clusterName,
	}
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   svcName,
			Labels: labels,
			Annotations: map[string]string{
				TolerateUnreadyEndpointsAnnotation: "true",
			},
		},
		Spec: v1.ServiceSpec{
			Ports:     ports,
			Selector:  labels,
			ClusterIP: clusterIP,
		},
	}
	return svc
}

func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}

// NewNatsPodSpec returns a NATS peer pod specification, based on the cluster specification.
func NewNatsPodSpec(clusterName string, cs spec.ClusterSpec, owner metav1.OwnerReference) *v1.Pod {
	// TODO add TLS, auth support, debug and tracing
	args := []string{
		fmt.Sprintf("--cluster=nats://0.0.0.0:%d", constants.ClusterPort),
		fmt.Sprintf("--http_port=%d", constants.MonitoringPort),
		fmt.Sprintf("--routes=nats://%s:%d", clusterName+"-mgmt", constants.ClusterPort),
		// TODO cs.TLS.IsSecureClient and cs.TLS.IsSecurePeer
	}

	labels := map[string]string{
		"app":          "nats",
		"nats_cluster": clusterName,
	}

	volumes := []v1.Volume{}

	container := containerWithLivenessProbe(natsPodContainer(args, cs.Version), natsLivenessProbe(cs.TLS.IsSecureClient()))

	if cs.Pod != nil {
		container = containerWithRequirements(container, cs.Pod.Resources)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:       labels,
			GenerateName: fmt.Sprintf("nats-%s-", clusterName),
			Annotations:  map[string]string{},
		},
		Spec: v1.PodSpec{
			Containers:    []v1.Container{container},
			RestartPolicy: v1.RestartPolicyNever,
			Volumes:       volumes,
		},
	}

	applyPodPolicy(clusterName, pod, cs.Pod)

	SetNATSVersion(pod, cs.Version)

	addOwnerRefToObject(pod.GetObjectMeta(), owner)

	return pod
}

func MustNewKubeClient() corev1client.CoreV1Interface {
	cfg, err := InClusterConfig()
	if err != nil {
		panic(err)
	}
	return corev1client.NewForConfigOrDie(cfg)
}

func InClusterConfig() (*rest.Config, error) {
	// Work around https://github.com/kubernetes/kubernetes/issues/40973
	if len(os.Getenv("KUBERNETES_SERVICE_HOST")) == 0 {
		addrs, err := net.LookupHost("kubernetes.default.svc")
		if err != nil {
			panic(err)
		}
		os.Setenv("KUBERNETES_SERVICE_HOST", addrs[0])
	}
	if len(os.Getenv("KUBERNETES_SERVICE_PORT")) == 0 {
		os.Setenv("KUBERNETES_SERVICE_PORT", "443")
	}
	return rest.InClusterConfig()
}

func IsKubernetesResourceAlreadyExistError(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func IsKubernetesResourceNotFoundError(err error) bool {
	return apierrors.IsNotFound(err)
}

// We are using internal api types for cluster related.
func ClusterListOpt(clusterName string) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(LabelsForCluster(clusterName)).String(),
	}
}

func LabelsForCluster(clusterName string) map[string]string {
	return map[string]string{
		"nats_cluster": clusterName,
		"app":          "nats",
	}
}

func CreatePatch(o, n, datastruct interface{}) ([]byte, error) {
	oldData, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	newData, err := json.Marshal(n)
	if err != nil {
		return nil, err
	}
	return strategicpatch.CreateTwoWayMergePatch(oldData, newData, datastruct)
}

func ClonePod(p *v1.Pod) *v1.Pod {
	np, err := scheme.Scheme.DeepCopy(p)
	if err != nil {
		panic("cannot deep copy pod")
	}
	return np.(*v1.Pod)
}

func CascadeDeleteOptions(gracePeriodSeconds int64) *metav1.DeleteOptions {
	return &metav1.DeleteOptions{
		GracePeriodSeconds: func(t int64) *int64 { return &t }(gracePeriodSeconds),
		PropagationPolicy: func() *metav1.DeletionPropagation {
			foreground := metav1.DeletePropagationForeground
			return &foreground
		}(),
	}
}

// mergeLables merges l2 into l1. Conflicting label will be skipped.
func mergeLabels(l1, l2 map[string]string) {
	for k, v := range l2 {
		if _, ok := l1[k]; ok {
			continue
		}
		l1[k] = v
	}
}
