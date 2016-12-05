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

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pires/nats-operator/pkg/spec"
	"github.com/pires/nats-operator/pkg/util/k8sutil"
	"github.com/pires/nats-operator/test/e2e/framework"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
	k8sclient "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/util/wait"
)

func waitUntilSizeReached(f *framework.Framework, clusterName string, size int, timeout time.Duration) ([]string, error) {
	return waitSizeReachedWithFilter(f, clusterName, size, timeout, func(*api.Pod) bool {
		return true
	})
}

func waitSizeReachedWithFilter(f *framework.Framework, clusterName string, size int, timeout time.Duration, podValidationFunc func(*api.Pod) bool) ([]string, error) {
	var names []string
	err := wait.Poll(5*time.Second, timeout, func() (done bool, err error) {
		podList, err := f.KubeClient.Pods(f.Namespace.Name).List(k8sutil.PodListOpt(clusterName))
		if err != nil {
			return false, err
		}
		names = nil
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Status.Phase == api.PodRunning {
				names = append(names, pod.Name)
			}
		}
		fmt.Printf("waiting size (%d), pods: %v\n", size, names)
		if len(names) != size {
			return false, nil
		}

		upgraded := 0
		for i := range podList.Items {
			pod := &podList.Items[i]
			if podValidationFunc(pod) {
				upgraded++
			}
		}
		if upgraded != size {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func killMembers(f *framework.Framework, names ...string) error {
	for _, name := range names {
		err := f.KubeClient.Pods(f.Namespace.Name).Delete(name, api.NewDeleteOptions(0))
		if err != nil {
			return err
		}
	}
	return nil
}

func makeClusterSpec(genName string, size int) *spec.NatsCluster {
	return &spec.NatsCluster{
		TypeMeta: unversioned.TypeMeta{
			APIVersion: "nats.io/v1",
			Kind:       "NatsCluster",
		},
		ObjectMeta: api.ObjectMeta{
			GenerateName: genName,
		},
		Spec: spec.ClusterSpec{
			Size: size,
			// TODO enable persistent storage when streaming is enabled
		},
	}
}

func clusterWithVersion(ec *spec.NatsCluster, version string) *spec.NatsCluster {
	ec.Spec.Version = version
	return ec
}

func createCluster(f *framework.Framework, e *spec.NatsCluster) (*spec.NatsCluster, error) {
	fmt.Printf("creating NATS cluster: %v\n", e.ClusterName)
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	resp, err := f.KubeClient.Client.Post(
		fmt.Sprintf("%s/apis/nats.io/v1/namespaces/%s/natsclusters", f.MasterHost, f.Namespace.Name),
		"application/json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status: %v", resp.Status)
	}
	decoder := json.NewDecoder(resp.Body)
	res := &spec.NatsCluster{}
	if err := decoder.Decode(res); err != nil {
		return nil, err
	}
	fmt.Printf("created NATS cluster: %v\n", res.Name)
	return res, nil
}

func updateCluster(f *framework.Framework, e *spec.NatsCluster) (*spec.NatsCluster, error) {
	fmt.Printf("updating NATS cluster: %v\n", e.ClusterName)
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("PUT",
		fmt.Sprintf("%s/apis/nats.io/v1/namespaces/%s/natsclusters/%s", f.MasterHost, f.Namespace.Name, e.Name),
		bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.KubeClient.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %v", resp.Status)
	}
	decoder := json.NewDecoder(resp.Body)
	res := &spec.NatsCluster{}
	if err := decoder.Decode(res); err != nil {
		return nil, err
	}

	fmt.Printf("updated NATS cluster: %v\n", res.Name)

	return res, nil
}

func deleteCluster(f *framework.Framework, name string) error {
	fmt.Printf("deleting NATS cluster: %v\n", name)
	podList, err := f.KubeClient.Pods(f.Namespace.Name).List(k8sutil.PodListOpt(name))
	if err != nil {
		return err
	}
	fmt.Println("NATS pods ======")
	for i := range podList.Items {
		pod := &podList.Items[i]
		fmt.Printf("pod (%v): status (%v)\n", pod.Name, pod.Status.Phase)
		buf := bytes.NewBuffer(nil)

		if pod.Status.Phase == api.PodFailed {
			if err := getLogs(f.KubeClient, f.Namespace.Name, pod.Name, "nats", buf); err != nil {
				return err
			}
			fmt.Println(pod.Name, "logs ===")
			fmt.Println(buf.String())
			fmt.Println(pod.Name, "logs END ===")
		}
	}

	buf := bytes.NewBuffer(nil)
	if err := getLogs(f.KubeClient, f.Namespace.Name, "nats-operator", "nats-operator", buf); err != nil {
		return err
	}
	fmt.Println("nats-operator logs ===")
	fmt.Println(buf.String())
	fmt.Println("nats-operator logs END ===")

	req, err := http.NewRequest("DELETE",
		fmt.Sprintf("%s/apis/nats.io/v1/namespaces/%s/natsclusters/%s", f.MasterHost, f.Namespace.Name, name), nil)
	if err != nil {
		return err
	}
	resp, err := f.KubeClient.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %v", resp.Status)
	}
	return nil
}

func getLogs(kubecli *k8sclient.Client, ns, p, c string, out io.Writer) error {
	req := kubecli.RESTClient.Get().
		Namespace(ns).
		Resource("pods").
		Name(p).
		SubResource("log").
		Param("container", c).
		Param("tailLines", "20")

	readCloser, err := req.Stream()
	if err != nil {
		return err
	}
	defer readCloser.Close()

	_, err = io.Copy(out, readCloser)
	return err
}
