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

func TestCreateTLSSetup(t *testing.T) {
	t.Skip()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runController(ctx, t)

	cl, err := newKubeClients()
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the CRD to be registered in the background.
	time.Sleep(10 * time.Second)

	name := "nats"
	namespace := "default"
	var size = 3
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
			TLS: &spec.TLSConfig{
				ServerSecret: "nats-certs",
				RoutesSecret: "nats-routes-tls",
			},
		},
	}
	_, err = cl.ncli.Create(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = cl.ncli.Delete(ctx, namespace, name)
		if err != nil {
			t.Fatal(err)
		}
	}()

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

		for _, pod := range podList.Items {
			sinceTime := k8smetav1.NewTime(time.Now().Add(time.Duration(-1 * time.Hour)))
			podName := pod.Name
			opts := &k8sv1.PodLogOptions{SinceTime: &sinceTime}
			rc, err := cl.kc.Pods(namespace).GetLogs(podName, opts).Stream()
			if err != nil {
				t.Fatalf("Logs request has failed: %v", err)
			}
			buf := new(bytes.Buffer)
			buf.ReadFrom(rc)
			output := buf.String()
			rc.Close()

			expected := 3
			got := strings.Count(output, "Route connection created")
			if got < expected {
				t.Logf("OUTPUT: %s", output)
				return false, nil
			}
		}

		return true, nil
	})
	if err != nil {
		t.Errorf("Error waiting for pods to be created: %s", err)
	}
	// Give some time for cluster to form
	time.Sleep(2 * time.Second)

	cm, err := cl.kc.Secrets(namespace).Get(name, k8smetav1.GetOptions{})
	if err != nil {
		t.Errorf("Config map error: %v", err)
	}
	conf, ok := cm.Data["nats.conf"]
	if !ok {
		t.Error("Config map was missing")
	}
	for _, pod := range podList.Items {
		if !strings.Contains(string(conf), pod.Name) {
			t.Errorf("Could not find pod %q in config", pod.Name)
		}

		sinceTime := k8smetav1.NewTime(time.Now().Add(time.Duration(-1 * time.Hour)))
		podName := pod.Name
		opts := &k8sv1.PodLogOptions{SinceTime: &sinceTime}
		rc, err := cl.kc.Pods(namespace).GetLogs(podName, opts).Stream()
		if err != nil {
			t.Fatalf("Logs request has failed: %v", err)
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)

		output := buf.String()

		if !strings.Contains(output, "TLS required for client connections") {
			t.Fatalf("Expected TLS to be required for clients")
		}
		expected := 3
		got := strings.Count(output, "Route connection created")
		if got < expected {
			t.Fatalf("Expected TLS for routes with at least %d connections to be created, got: %d", expected, got)
		}
		rc.Close()
	}
}
