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

package kubernetes

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/debug/local"
	"github.com/nats-io/nats-operator/pkg/spec"
	"github.com/nats-io/nats-operator/pkg/util/retryutil"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8srand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // for gcp auth
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	TolerateUnreadyEndpointsAnnotation = "service.alpha.kubernetes.io/tolerate-unready-endpoints"
	versionAnnotationKey               = "nats.version"
)

const (
	LabelAppKey            = "app"
	LabelAppValue          = "nats"
	LabelClusterNameKey    = "nats_cluster"
	LabelClusterVersionKey = "nats_version"
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

func createService(kubecli corev1client.CoreV1Interface, svcName, clusterName, ns, clusterIP string, ports []v1.ServicePort, owner metav1.OwnerReference, selectors map[string]string, tolerateUnready bool) error {
	svc := newNatsServiceManifest(svcName, clusterName, clusterIP, ports, selectors, tolerateUnready)
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
	selectors := LabelsForCluster(clusterName)
	return createService(kubecli, clusterName+"-client", clusterName, ns, "", ports, owner, selectors, false)
}

func ManagementServiceName(clusterName string) string {
	return clusterName
}

// CreateMgmtService creates an headless service for NATS management purposes.
func CreateMgmtService(kubecli corev1client.CoreV1Interface, clusterName, clusterVersion, ns string, owner metav1.OwnerReference) error {
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
	selectors := LabelsForCluster(clusterName)
	selectors[LabelClusterVersionKey] = clusterVersion
	return createService(kubecli, ManagementServiceName(clusterName), clusterName, ns, v1.ClusterIPNone, ports, owner, selectors, true)
}

// CreateConfigMap creates the config map that is shared by NATS servers in a cluster.
func CreateConfigMap(kubecli corev1client.CoreV1Interface, clusterName, ns string, cluster spec.ClusterSpec, owner metav1.OwnerReference) error {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Data: map[string]string{
			"nats.conf": "",
		},
	}
	addOwnerRefToObject(cm.GetObjectMeta(), owner)

	_, err := kubecli.ConfigMaps(ns).Create(cm)
	return err
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

func newNatsServiceManifest(svcName, clusterName, clusterIP string, ports []v1.ServicePort, selectors map[string]string, tolerateUnready bool) *v1.Service {
	labels := map[string]string{
		LabelAppKey:         LabelAppValue,
		LabelClusterNameKey: clusterName,
	}

	annotations := make(map[string]string)
	if tolerateUnready == true {
		annotations[TolerateUnreadyEndpointsAnnotation] = "true"
	}

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svcName,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Ports:     ports,
			Selector:  selectors,
			ClusterIP: clusterIP,
		},
	}
}

func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}

// NewNatsPodSpec returns a NATS peer pod specification, based on the cluster specification.
func NewNatsPodSpec(clusterName string, cs spec.ClusterSpec, owner metav1.OwnerReference) *v1.Pod {
	labels := map[string]string{
		LabelAppKey:            "nats",
		LabelClusterNameKey:    clusterName,
		LabelClusterVersionKey: cs.Version,
	}

	// Mount the config map that ought to have been created
	// for the pods in the cluster.
	volumes := []v1.Volume{}

	container := natsPodContainer(clusterName, cs.Version)
	container = containerWithLivenessProbe(container, natsLivenessProbe())

	if cs.Pod != nil {
		container = containerWithRequirements(container, cs.Pod.Resources)
	}
	name := UniquePodName()

	// Rely on the shared configuration map for configuring the cluster.
	cmd := []string{
		"/gnatsd",
		"-c",
		"/etc/nats-config/nats.conf",
	}

	container.Command = cmd

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: map[string]string{},
		},
		Spec: v1.PodSpec{
			Hostname:      name,
			Subdomain:     clusterName,
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
	var (
		cfg *rest.Config
		err error
	)

	if len(local.KubeConfigPath) == 0 {
		cfg, err = InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", local.KubeConfigPath)
	}

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
		LabelAppKey:         LabelAppValue,
		LabelClusterNameKey: clusterName,
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

func CloneSvc(s *v1.Service) *v1.Service {
	ns, err := scheme.Scheme.DeepCopy(s)
	if err != nil {
		panic("cannot deep copy svc")
	}
	return ns.(*v1.Service)
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

// UniquePodName generates a unique name for the Pod.
func UniquePodName() string {
	return fmt.Sprintf("nats-%s", k8srand.String(10))
}
