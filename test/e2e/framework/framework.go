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
	"flag"
	"fmt"
	"time"

	"github.com/nats-io/nats-operator/pkg/client"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	"github.com/nats-io/nats-operator/pkg/util/probe"
	"github.com/nats-io/nats-operator/pkg/util/retryutil"
	"github.com/nats-io/nats-operator/test/e2e/e2eutil"

	"github.com/sirupsen/logrus"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

var Global *Framework

type Framework struct {
	opImage    string
	KubeClient corev1.CoreV1Interface
	CRClient   client.NatsClusterCR
	Namespace  string
}

// Setup setups a test framework and points "Global" to it.
func Setup() error {
	kubeconfig := flag.String("kubeconfig", "", "kube config path, e.g. $HOME/.kube/config")
	opImage := flag.String("operator-image", "", "operator image, e.g. nats-io/nats-operator")
	ns := flag.String("namespace", "default", "e2e test namespace")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return err
	}
	cli, err := corev1.NewForConfig(config)
	if err != nil {
		return err
	}
	crClient, err := client.NewCRClient(config)
	if err != nil {
		return err
	}

	Global = &Framework{
		KubeClient: cli,
		CRClient:   crClient,
		Namespace:  *ns,
		opImage:    *opImage,
	}
	return Global.setup()
}

func Teardown() error {
	if err := Global.deleteNatsOperator(); err != nil {
		return err
	}
	// TODO: check all deleted and wait
	Global = nil
	logrus.Info("e2e teardown successfully")
	return nil
}

func (f *Framework) setup() error {
	if err := f.SetupNatsOperator(); err != nil {
		return fmt.Errorf("failed to setup NATS operator: %v", err)
	}
	logrus.Info("NATS operator created successfully")

	logrus.Info("e2e setup successfully")
	return nil
}

func (f *Framework) SetupNatsOperator() error {
	// TODO: unify this and the yaml file in example/
	cmd := []string{"/usr/local/bin/nats-operator"}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "nats-operator",
			Labels: map[string]string{"name": "nats-operator"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:            "nats-operator",
					Image:           f.opImage,
					ImagePullPolicy: v1.PullAlways,
					Command:         cmd,
					Env: []v1.EnvVar{
						{
							Name:      "MY_POD_NAMESPACE",
							ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
						},
						{
							Name:      "MY_POD_NAME",
							ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.name"}},
						},
					},
					ReadinessProbe: &v1.Probe{
						Handler: v1.Handler{
							HTTPGet: &v1.HTTPGetAction{
								Path: probe.HTTPReadyzEndpoint,
								Port: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
							},
						},
						InitialDelaySeconds: 3,
						PeriodSeconds:       3,
						FailureThreshold:    3,
					},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}

	p, err := kubernetesutil.CreateAndWaitPod(f.KubeClient, f.Namespace, pod, 60*time.Second)
	if err != nil {
		return err
	}
	logrus.Infof("NATS operator pod is running on node (%s)", p.Spec.NodeName)

	return e2eutil.WaitUntilOperatorReady(f.KubeClient, f.Namespace, "nats-operator")
}

func (f *Framework) DeleteNatsOperatorCompletely() error {
	err := f.deleteNatsOperator()
	if err != nil {
		return err
	}
	// On k8s 1.6.1, grace period isn't accurate. It took ~10s for operator pod to completely disappear.
	// We work around by increasing the wait time. Revisit this later.
	err = retryutil.Retry(5*time.Second, 6, func() (bool, error) {
		_, err := f.KubeClient.Pods(f.Namespace).Get("nats-operator", metav1.GetOptions{})
		if err == nil {
			return false, nil
		}
		if kubernetesutil.IsKubernetesResourceNotFoundError(err) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		return fmt.Errorf("fail to wait NATS operator pod gone from API: %v", err)
	}
	return nil
}

func (f *Framework) deleteNatsOperator() error {
	return f.KubeClient.Pods(f.Namespace).Delete("nats-operator", metav1.NewDeleteOptions(1))
}
