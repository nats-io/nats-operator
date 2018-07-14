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
				EnableConfigReload: true,
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

func TestConfigSecretReload_Auth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
	authSecretName := fmt.Sprintf("%s-clients-auth", name)
	cm := &k8sv1.Secret{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name: authSecretName,
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
				EnableConfigReload: true,
			},
			Auth: &spec.AuthConfig{
				ClientsAuthSecret:  authSecretName,
				ClientsAuthTimeout: 10,
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
		pod := podList.Items[0]
		if pod.Status.Phase != k8sv1.PodRunning {
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
					Image:           "wallyqs/nats-ops:tools",
					ImagePullPolicy: k8sv1.PullIfNotPresent,
					Command: []string{
						"/nats-sub",
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

	// Should poll until pod is available.
	err = k8swaitutil.Poll(3*time.Second, 1*time.Minute, func() (bool, error) {
		pod2, err := cl.kc.Pods(namespace).Get(opsPodName, k8smetav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if pod2.Status.Phase != k8sv1.PodRunning {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Errorf("Error waiting for pods to be created: %s", err)
	}

	// Confirm that the pod subscribed successfully, then do a reload
	// removing its user.
	k8swaitutil.Poll(3*time.Second, 30*time.Second, func() (bool, error) {
		sinceTime := k8smetav1.NewTime(time.Now().Add(time.Duration(-1 * time.Hour)))
		opts := &k8sv1.PodLogOptions{
			SinceTime: &sinceTime,
		}
		rc, err := cl.kc.Pods(namespace).GetLogs(opsPodName, opts).Stream()
		if err != nil {
			return false, err
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)

		output := buf.String()
		t.Logf("OUTPUT: %s", output)
		if !strings.Contains(output, "Listening on [hello.world]") {
			return false, nil
		}
		return true, nil
	})

	// Remove the user and then current connection will be closed.
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
	result, err := cl.kc.Secrets(namespace).Get(authSecretName, k8smetav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result.Data["anything"] = []byte(sec)
	_, err = cl.kc.Secrets(namespace).Update(result)
	if err != nil {
		t.Fatal(err)
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

		// On reload now pod would get:
		//
		// Authorization Error - User "user1"
		//
		if !strings.Contains(output, "Reloaded: authorization users") {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Errorf("Error waiting for pods to be reloaded: %s", err)
	}
}

func TestConfigNatsServiceRolesReload_Auth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	runController(ctx, t)
	cl, err := newKubeClients()
	if err != nil {
		t.Fatal(err)
	}

	var (
		userRoleName      = "nats-user-2"
		adminUserRoleName = "nats-admin-user"
		namespace         = "default"
		clusterName       = "test-nats-roles-cluster-reload-auth-2"
	)
	userRole := &spec.NatsServiceRole{
		TypeMeta: k8smetav1.TypeMeta{
			Kind:       spec.ServiceRoleCRDResourceKind,
			APIVersion: spec.SchemeGroupVersion.String(),
		},
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      userRoleName,
			Namespace: namespace,
			Labels:    map[string]string{"nats_cluster": clusterName},
		},
		Spec: spec.ServiceRoleSpec{
			Permissions: spec.Permissions{
				Publish:   []string{"foo.bar"},
				Subscribe: []string{"foo.*"},
			},
		},
	}
	userServiceAccount := &k8sv1.ServiceAccount{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      userRoleName,
			Namespace: namespace,
		},
	}

	adminUserRole := &spec.NatsServiceRole{
		TypeMeta: k8smetav1.TypeMeta{
			Kind:       spec.ServiceRoleCRDResourceKind,
			APIVersion: spec.SchemeGroupVersion.String(),
		},
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      adminUserRoleName,
			Namespace: namespace,
			Labels:    map[string]string{"nats_cluster": clusterName},
		},
		Spec: spec.ServiceRoleSpec{
			Permissions: spec.Permissions{
				Publish:   []string{">"},
				Subscribe: []string{">"},
			},
		},
	}
	adminUserServiceAccount := &k8sv1.ServiceAccount{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      adminUserRoleName,
			Namespace: namespace,
		},
	}

	// Wait for the CRD to be registered in the background.
	time.Sleep(10 * time.Second)

	_, err = cl.kc.ServiceAccounts(namespace).Create(userServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cl.ocli.NatsServiceRoles(namespace).Create(userRole)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cl.kc.ServiceAccounts(namespace).Create(adminUserServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cl.ocli.NatsServiceRoles(namespace).Create(adminUserRole)
	if err != nil {
		t.Fatal(err)
	}

	// Create cluster with service accounts support enabled.
	var size = 1
	cluster := &spec.NatsCluster{
		TypeMeta: k8smetav1.TypeMeta{
			Kind:       spec.CRDResourceKind,
			APIVersion: spec.SchemeGroupVersion.String(),
		},
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: spec.ClusterSpec{
			Size:    size,
			Version: "1.1.0",
			Pod: &spec.PodPolicy{
				EnableConfigReload: true,
			},
			Auth: &spec.AuthConfig{
				EnableServiceAccounts: true,
				ClientsAuthTimeout:    10,
			},
		},
	}
	_, err = cl.ncli.Create(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the pods to be created.
	params := k8smetav1.ListOptions{
		LabelSelector: fmt.Sprintf("nats_cluster=%s", clusterName),
	}
	boundTokenSecretName := fmt.Sprintf("%s-%s-bound-token", userRoleName, clusterName)
	var podList *k8sv1.PodList
	k8swaitutil.Poll(3*time.Second, 1*time.Minute, func() (bool, error) {
		podList, err = cl.kc.Pods(namespace).List(params)
		if err != nil {
			return false, err
		}
		if len(podList.Items) < size {
			return false, nil
		}
		pod := podList.Items[0]
		if pod.Status.Phase != k8sv1.PodRunning {
			return false, nil
		}

		// Wait for the secret to be created to be able to mount it.
		_, err := cl.kc.Secrets(namespace).Get(boundTokenSecretName, k8smetav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})

	// Create pod that will be mounting the created secret.
	opsPodName := fmt.Sprintf("%s-%s-account-ops-1", userRoleName, clusterName)
	volumes := make([]k8sv1.Volume, 0)
	volume := k8sv1.Volume{
		Name: "token-volume",
		VolumeSource: k8sv1.VolumeSource{
			Secret: &k8sv1.SecretVolumeSource{
				SecretName: boundTokenSecretName,
			},
		},
	}
	volumes = append(volumes, volume)
	volumeMount := k8sv1.VolumeMount{
		Name:      "token-volume",
		MountPath: "/nats-token",
	}
	volumeMounts := make([]k8sv1.VolumeMount, 0)
	volumeMounts = append(volumeMounts, volumeMount)
	pod := &k8sv1.Pod{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:   opsPodName,
			Labels: map[string]string{"name": "nats-ops"},
		},
		Spec: k8sv1.PodSpec{
			Volumes: volumes,
			Containers: []k8sv1.Container{
				{
					Name:            opsPodName,
					Image:           "wallyqs/nats-ops:latest",
					ImagePullPolicy: k8sv1.PullIfNotPresent,
					Command:         []string{"/bin/sh", "-c"},
					Args: []string{
						fmt.Sprintf(
							"/go/bin/nats-sub -s nats://%s:`cat /nats-token/token`@%s:4222 hello.world;",
							userRoleName,
							clusterName),
					},
					VolumeMounts: volumeMounts,
				},
			},
		},
	}
	_, err = cl.kc.Pods(namespace).Create(pod)
	if err != nil {
		t.Fatal(err)
	}

	// Should poll until pod is available
	k8swaitutil.Poll(3*time.Second, 1*time.Minute, func() (bool, error) {
		pod2, err := cl.kc.Pods(namespace).Get(opsPodName, k8smetav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if pod2.Status.Phase != k8sv1.PodRunning {
			return false, nil
		}
		return true, nil
	})

	// Confirm that the pod did not have permissions, then do a reload
	// adding permissions for its user.
	k8swaitutil.Poll(3*time.Second, 30*time.Second, func() (bool, error) {
		sinceTime := k8smetav1.NewTime(time.Now().Add(time.Duration(-1 * time.Hour)))
		opts := &k8sv1.PodLogOptions{
			SinceTime: &sinceTime,
		}
		rc, err := cl.kc.Pods(namespace).GetLogs(opsPodName, opts).Stream()
		if err != nil {
			return false, err
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)

		output := buf.String()
		t.Logf("OUTPUT: %s", output)
		if !strings.Contains(output, "permissions violation for subscription to \"hello.world\"") {
			return false, nil
		}
		return true, nil
	})

	// Update the permissions from role so that it can now connect.
	userRole, err = cl.ocli.NatsServiceRoles(namespace).Get(userRoleName, k8smetav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	userRole.Spec.Permissions = spec.Permissions{
		Publish:   []string{"foo.bar"},
		Subscribe: []string{"foo.*", "hello.world"},
	}
	_, err = cl.ocli.NatsServiceRoles(namespace).Update(userRole)
	if err != nil {
		t.Fatal(err)
	}

	opsPodName = fmt.Sprintf("%s-%s-account-ops-2", userRoleName, clusterName)
	pod = &k8sv1.Pod{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:   opsPodName,
			Labels: map[string]string{"name": "nats-ops"},
		},
		Spec: k8sv1.PodSpec{
			Volumes: volumes,
			Containers: []k8sv1.Container{
				{
					Name:            opsPodName,
					Image:           "wallyqs/nats-ops:latest",
					ImagePullPolicy: k8sv1.PullIfNotPresent,
					Command:         []string{"/bin/sh", "-c"},
					Args: []string{
						fmt.Sprintf(
							"/go/bin/nats-sub -s nats://%s:`cat /nats-token/token`@%s:4222 hello.world",
							userRoleName,
							clusterName),
					},
					VolumeMounts: volumeMounts,
				},
			},
		},
	}
	_, err = cl.kc.Pods(namespace).Create(pod)
	if err != nil {
		t.Fatal(err)
	}

	// Should poll until pod is available
	k8swaitutil.Poll(3*time.Second, 30*time.Second, func() (bool, error) {
		pod2, err := cl.kc.Pods(namespace).Get(opsPodName, k8smetav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if pod2.Status.Phase != k8sv1.PodRunning {
			return false, nil
		}
		return true, nil
	})

	// Confirm that the pod did not have permissions, then do a reload
	// adding permissions for its user.
	k8swaitutil.Poll(3*time.Second, 30*time.Second, func() (bool, error) {
		sinceTime := k8smetav1.NewTime(time.Now().Add(time.Duration(-1 * time.Hour)))
		opts := &k8sv1.PodLogOptions{
			SinceTime: &sinceTime,
		}
		rc, err := cl.kc.Pods(namespace).GetLogs(opsPodName, opts).Stream()
		if err != nil {
			return false, err
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)

		output := buf.String()
		t.Logf("OUTPUT: %s", output)
		if !strings.Contains(output, "Listening on [hello.world]") {
			return false, nil
		}
		return true, nil
	})

	// Next, delete the role which will cause a disconnection.
	err = cl.ocli.NatsServiceRoles(namespace).Delete(userRoleName, &k8smetav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	k8swaitutil.Poll(3*time.Second, 1*time.Minute, func() (bool, error) {
		sinceTime := k8smetav1.NewTime(time.Now().Add(time.Duration(-1 * time.Hour)))
		opts := &k8sv1.PodLogOptions{
			SinceTime: &sinceTime,
		}
		rc, err := cl.kc.Pods(namespace).GetLogs(opsPodName, opts).Stream()
		if err != nil {
			return false, err
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)

		output := buf.String()
		t.Logf("OUTPUT: %s", output)
		if !strings.Contains(output, "disconnected") {
			return false, nil
		}
		return true, nil
	})
}
