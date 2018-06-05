package operatortests

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-operator/pkg/spec"
	k8sv1 "k8s.io/api/core/v1"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8swaitutil "k8s.io/apimachinery/pkg/util/wait"
)

func TestConfigMapReload_Servers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	runController(ctx, t)

	cl, err := newKubeClients()
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the CRD to be registered in the background.
	time.Sleep(10 * time.Second)
	name := "test-nats-cluster-reload-1"
	namespace := "default"

	// Start with a single node, then wait for the reload event
	// due to increasing size of the cluster.
	var size = 1
	cluster := &spec.NatsCluster{
		TypeMeta: k8smetav1.TypeMeta{
			Kind:       spec.CRDResourceKind,
			APIVersion: spec.SchemeGroupVersion.String(),
		},
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec.ClusterSpec{
			Size:    size,
			Version: "1.1.0",
			Pod: &spec.PodPolicy{
				AllowConfigReload: true,
			},
		},
	}
	_, err = cl.ncli.Create(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the pods to be created
	params := k8smetav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=nats,nats_cluster=%s", name),
	}
	var podList *k8sv1.PodList
	err = k8swaitutil.Poll(3*time.Second, 1*time.Minute, func() (bool, error) {
		podList, err = cl.kc.Pods(namespace).List(params)
		if err != nil {
			return false, err
		}
		if len(podList.Items) < size {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Errorf("Error waiting for pods to be created: %s", err)
	}

	// Now scale up and resize the cluster which will trigger a reload,
	// for that need to get first and to be able to make the update.
	size = 3
	cluster, err = cl.ncli.Get(ctx, namespace, name)
	if err != nil {
		t.Fatal(err)
	}
	cluster.Spec.Size = size

	_, err = cl.ncli.Update(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the pods to be created
	params = k8smetav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=nats,nats_cluster=%s", name),
	}
	err = k8swaitutil.Poll(3*time.Second, 3*time.Minute, func() (bool, error) {
		podList, err = cl.kc.Pods(namespace).List(params)
		if err != nil {
			return false, err
		}
		if len(podList.Items) < size {
			return false, nil
		}

		sinceTime := k8smetav1.NewTime(time.Now().Add(time.Duration(-1 * time.Hour)))
		opts := &k8sv1.PodLogOptions{
			SinceTime: &sinceTime,
			Container: "nats",
		}
		rc, err := cl.kc.Pods(namespace).GetLogs(fmt.Sprintf("%s-1", name), opts).Stream()
		if err != nil {
			t.Fatalf("Logs request has failed: %v", err)
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)

		output := buf.String()

		if !strings.Contains(output, "Reloaded server configuration") {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Errorf("Error waiting for pods to be reloaded: %s", err)
	}
	time.Sleep(1 * time.Minute)
}
