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

func TestConfigMapReload_Auth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	runController(ctx, t)

	cl, err := newKubeClients()
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the CRD to be registered in the background.
	time.Sleep(10 * time.Second)
	name := "test-nats-cluster-reload-auth-1"
	namespace := "default"

	// Create the secret with the auth credentials
	sec := `{
  "users": [
    { "username": "user1", "password": "secret1",
      "permissions": {
	"publish": ["hello.*"],
	"subscribe": ["hello.world"]
      }
    }
  ],
  "default_permissions": {
    "publish": ["SANDBOX.*"],
    "subscribe": ["PUBLIC.>"]
  }
}
`
	cm := &k8sv1.Secret{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name: name,
		},
		Data: map[string][]byte{
			"anything": []byte(sec),
		},
	}
	_, err = cl.kc.Secrets(namespace).Create(cm)
	if err != nil {
		t.Fatal(err)
	}

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
			Auth: &spec.AuthConfig{
				ClientsAuthSecret:  name,
				ClientsAuthTimeout: 5,
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

	opsPodName := fmt.Sprintf("%s-ops-1", name)
	pod := &k8sv1.Pod{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:   opsPodName,
			Labels: map[string]string{"name": "nats-ops"},
		},
		Spec: k8sv1.PodSpec{
			Containers: []k8sv1.Container{
				{
					Name:            opsPodName,
					Image:           "wallyqs/nats-ops:latest",
					ImagePullPolicy: k8sv1.PullAlways,
					Command: []string{
						"nats-sub",
						"-s",
						fmt.Sprintf("nats://user1:secret1@%s:4222", name),
						"hello.world",
					},
				},
			},
		},
	}
	_, err = cl.kc.Pods(namespace).Create(pod)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Second)
	{
		sinceTime := k8smetav1.NewTime(time.Now().Add(time.Duration(-1 * time.Hour)))
		opts := &k8sv1.PodLogOptions{
			SinceTime: &sinceTime,
		}
		rc, err := cl.kc.Pods(namespace).GetLogs(opsPodName, opts).Stream()
		if err != nil {
			t.Fatalf("Logs request has failed: %v", err)
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)

		output := buf.String()
		if !strings.Contains(output, "Listening on [hello.world]") {
			t.Fatalf("Logs request has failed: %v", err)
		}

		// { "username": "user1", "password": "secret1",
		//   "permissions": {
		// 	"publish": ["hello.*"],
		// 	"subscribe": ["hi.world"]
		//   }
		// },

		sec = `{
  "users": [
    { "username": "user2", "password": "secret2" }
  ],
  "default_permissions": {
    "publish": ["SANDBOX.*"],
    "subscribe": ["PUBLIC.>"]
  }
}
`
		result, err := cl.kc.Secrets(namespace).Get(name, k8smetav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		result.Data["anything"] = []byte(sec)
		_, err = cl.kc.Secrets(namespace).Update(result)
		if err != nil {
			t.Fatal(err)
		}

	}

	// Wait for the pods to be updated with new auth creds.
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

		if !strings.Contains(output, "Authorization Error") {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Errorf("Error waiting for pods to be reloaded: %s", err)
	}
}
