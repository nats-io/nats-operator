// +build e2e

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

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"

	nats "github.com/nats-io/nats.go"
	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	natsconf "github.com/nats-io/nats-operator/pkg/conf"
	"github.com/nats-io/nats-operator/pkg/util/kubernetes"
	"github.com/nats-io/nats-operator/test/e2e/framework"
)

// TestConfigReloadOnResize creates a NatsCluster resource with size 1 and then scales it up to 3 members.
// It then waits for a log message on the very first pod indicating that the configuration has been reloaded (since its configuration secret has been updated).
func TestConfigReloadOnResize(t *testing.T) {
	// Skip the test if "ShareProcessNamespace" is not enabled.
	f.Require(t, framework.ShareProcessNamespace)

	var (
		initialSize = 1
		finalSize   = 3
		version     = "1.3.0"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with a single member and having configuration reloading enabled.
	natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-", initialSize, version, func(natsCluster *natsv1alpha2.NatsCluster) {
		natsCluster.Spec.Pod = &natsv1alpha2.PodPolicy{
			// Enable configuration reloading.
			EnableConfigReload: true,
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(natsCluster); err != nil {
			t.Error(err)
		}
	}()

	// Wait until the full mesh is formed.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx1, natsCluster, initialSize, version); err != nil {
		t.Fatal(err)
	}

	// Scale the cluster up to three members
	natsCluster.Spec.Size = finalSize
	if natsCluster, err = f.PatchCluster(natsCluster); err != nil {
		t.Fatal(err)
	}

	// Make sure that the full mesh is formed with the current size.
	ctx2, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx2, natsCluster, finalSize, version); err != nil {
		t.Fatal(err)
	}

	// Wait for the "Reloaded: cluster routes" log message to appear in the logs for the very first pod.
	ctx3, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilPodLogLineMatches(ctx3, natsCluster, 1, "Reloaded: cluster routes"); err != nil {
		t.Fatal(err)
	}
}

// TestConfigReloadOnClientAuthSecretChange creates a secret containing authentication data for a NATS cluster.
// This secret initially contains two users ("user-1" and "user-2") and the corresponding password.
// Then, the test creates a NatsCluster resource that uses this secret for authentication, and makes sure that "user-1" can connect to the NATS cluster.
// Finally, it removes the entry that corresponds to "user-1" from the authentication secret, and makes sure that "user-1" cannot connect to the NATS cluster anymore.
func TestConfigReloadOnClientAuthSecretChange(t *testing.T) {
	// Skip the test if "ShareProcessNamespace" is not enabled.
	f.Require(t, framework.ShareProcessNamespace)

	// Create a NatsCluster resource with a single member, having configuration reloading enabled and using the secret above for client authentication.
	ConfigReloadTestHelper(t, func(natsCluster *natsv1alpha2.NatsCluster, cas *v1.Secret) {
		natsCluster.Spec.Auth = &natsv1alpha2.AuthConfig{
			// Use the secret created above for client authentication.
			ClientsAuthSecret: cas.Name,
		}
		natsCluster.Spec.Pod = &natsv1alpha2.PodPolicy{
			// Enable configuration reloading.
			EnableConfigReload: true,
		}
	})
}

// TestConfigReloadOnClientAuthSecretChange creates a secret containing authentication data for a NATS cluster.
// This secret initially contains two users ("user-1" and "user-2") and the corresponding password.
// Then, the test creates a NatsCluster resource that uses this secret for authentication, and makes sure that "user-1" can connect to the NATS cluster.
// Finally, it removes the entry that corresponds to "user-1" from the authentication secret, and makes sure that "user-1" cannot connect to the NATS cluster anymore.
func TestConfigReloadOnClientAuthFileChange(t *testing.T) {
	// Skip the test if "ShareProcessNamespace" is not enabled.
	f.Require(t, framework.ShareProcessNamespace)

	ConfigReloadTestHelper(t, func(natsCluster *natsv1alpha2.NatsCluster, cas *v1.Secret) {
		natsCluster.Spec.Auth = &natsv1alpha2.AuthConfig{
			// Use the secret created above for client authentication.
			ClientsAuthFile: "authconfig/auth.json",
		}
		natsCluster.Spec.Pod = &natsv1alpha2.PodPolicy{
			// Enable configuration reloading.
			EnableConfigReload:      true,
			ReloaderImage:           "wallyqs/nats-server-config-reloader",
			ReloaderImageTag:        "0.4.5-v1alpha2",
			ReloaderImagePullPolicy: "Always",
			VolumeMounts: []v1.VolumeMount{
				v1.VolumeMount{
					Name:      "authconfig",
					MountPath: "/etc/nats-config/authconfig",
				},
			},
		}
		natsCluster.Spec.PodTemplate = &v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Volumes: []v1.Volume{
					v1.Volume{
						Name: "authconfig",
						VolumeSource: v1.VolumeSource{
							Secret: &v1.SecretVolumeSource{
								SecretName: cas.Name,
								Items: []v1.KeyToPath{
									v1.KeyToPath{
										Key:  "data",
										Path: "auth.json",
									},
								},
							},
						},
					},
				},
			},
		}
	})
}

type NatsClusterCustomizerWSecret func(natsCluster *natsv1alpha2.NatsCluster, cas *v1.Secret)

func ConfigReloadTestHelper(t *testing.T, customizer NatsClusterCustomizerWSecret) {
	var (
		username1 = "user-1"
		username2 = "user-2"
		password1 = "pass-1"
		password2 = "pass-2"
		size      = 1
		version   = "1.4.0"
	)

	var (
		auth        natsconf.AuthorizationConfig
		c           *nats.Conn
		cas         *v1.Secret
		d           []byte
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create an object containing client authentication data for the NATS cluster.
	auth = natsconf.AuthorizationConfig{
		Users: []*natsconf.User{
			{
				User:     username1,
				Password: password1,
				Permissions: &natsconf.Permissions{
					Publish: []string{
						">",
					},
					Subscribe: []string{
						">",
					},
				},
			},
			{
				User:     username2,
				Password: password2,
				Permissions: &natsconf.Permissions{
					Publish: []string{
						">",
					},
					Subscribe: []string{
						">",
					},
				},
			},
		},
	}
	// Serialize the object containing authentication data,
	// we are using wildcard so need to unescape the HTML
	// which the JSON encoder does by default...
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	err = encoder.Encode(auth)
	if err != nil {
		t.Fatal(err)
	}
	buf2 := &bytes.Buffer{}
	err = json.Indent(buf2, buf.Bytes(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	// Create a secret containing authentication data.
	d = buf2.Bytes()
	if cas, err = f.CreateSecret(f.Namespace, "data", d); err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the secret after we're done testing.
	defer func() {
		if err = f.DeleteSecret(cas); err != nil {
			t.Error(err)
		}
	}()

	// Create a NatsCluster resource with a single member, having configuration reloading enabled and using the secret above for client authentication.
	natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-reload-", size, version, func(natsCluster *natsv1alpha2.NatsCluster) {
		customizer(natsCluster, cas)
	})

	if err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(natsCluster); err != nil {
			t.Error(err)

		}
	}()

	// Wait for the single pod to be created.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx1, natsCluster, size, version); err != nil {
		t.Fatal(err)
	}

	// Make sure that "user-1" can connect to the NATS cluster.
	if c, err = f.ConnectToNatsClusterWithUsernamePassword(natsCluster, username1, password1); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}

	// Remove "user1" from the list of allowed users.
	auth.Users = auth.Users[1:]

	// Serialize the object containing authentication data again.
	buf = &bytes.Buffer{}
	encoder = json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	err = encoder.Encode(auth)
	if err != nil {
		t.Fatal(err)
	}
	buf2 = &bytes.Buffer{}
	err = json.Indent(buf2, buf.Bytes(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	// Update the client authentication secret with the new contents.
	cas.Data["data"] = buf2.Bytes()
	if cas, err = f.PatchSecret(cas); err != nil {
		t.Fatal(err)
	}

	// Wait for the "Reloaded: authorization users" log message to appear in the logs for the single pod.
	ctx2, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilPodLogLineMatches(ctx2, natsCluster, 1, "Reloaded: authorization users"); err != nil {
		t.Fatal(err)
	}

	// Make sure that "user-1" CANNOT connect to the NATS cluster anymore.
	if _, err = f.ConnectToNatsClusterWithUsernamePassword(natsCluster, username1, password1); err == nil {
		t.Fatalf("expected connection from %q to have been rejected", username1)
	}

	// Make sure that "user-2" can still connect to the NATS cluster, as its authorization hasn't been revoked.
	if c, err = f.ConnectToNatsClusterWithUsernamePassword(natsCluster, username2, password2); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
}

// TestConfigReloadOnNatsServiceRoleUpdates creates two NatsServiceRole resources (nsr1 and nsr2) targeting a NatsCluster resource with a well-known name.
// It then created the NatsCluster resource and verifies that "nsr1" cannot subscribe to the "hello.world" subject.
// Finally, it adds "hello.world" to the list of allowed subjects for "nsr1" and verifies that "nsr1" can now subscribe to that subject.
func TestConfigReloadOnNatsServiceRoleUpdates(t *testing.T) {
	t.SkipNow()
	// Skip the test if "ShareProcessNamespace" or "TokenRequest" are not enabled.
	f.Require(t, framework.ShareProcessNamespace, framework.TokenRequest)

	var (
		clusterName = "test-nats-nsr"
		size        = 1
		subject     = "hello.world"
		version     = "1.3.0"
	)

	var (
		c           *nats.Conn
		nsr1        *natsv1alpha2.NatsServiceRole
		nsr2        *natsv1alpha2.NatsServiceRole
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsServiceRole resource having permissions to subscribe to "foo.bar" only.
	nsr1, err = f.CreateNatsServiceRole(f.Namespace, "test-nsr1-", func(nsr *natsv1alpha2.NatsServiceRole) {
		nsr.Spec.Permissions.Publish = []string{
			">",
		}
		nsr.Spec.Permissions.Subscribe = []string{
			"foo.bar",
		}
		// Set the target cluster name beforehand so that this NatsServiceRole is picked up when the NatsCluster resource is created.
		nsr.ObjectMeta.Labels[kubernetes.LabelClusterNameKey] = clusterName
	})
	// Make sure we cleanup the NatsServiceRole resource after we're done testing.
	defer func() {
		if err = f.DeleteNatsServiceRole(nsr1); err != nil {
			t.Error(err)
		}
	}()
	// Create a NatsServiceRole resource having full publish and subscribe permissions.
	nsr2, err = f.CreateNatsServiceRole(f.Namespace, "test-nsr2-", func(nsr *natsv1alpha2.NatsServiceRole) {
		nsr.Spec.Permissions.Publish = []string{
			">",
		}
		nsr.Spec.Permissions.Subscribe = []string{
			">",
		}
		// Set the target cluster name beforehand so that this NatsServiceRole is picked up when the NatsCluster resource is created.
		nsr.ObjectMeta.Labels[kubernetes.LabelClusterNameKey] = clusterName
	})
	// Make sure we cleanup the NatsServiceRole resource after we're done testing.
	defer func() {
		if err = f.DeleteNatsServiceRole(nsr2); err != nil {
			t.Error(err)
		}
	}()

	// Create a NatsCluster resource with a single member, having configuration reloading enabled and using service accounts for authentication.
	natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-", size, version, func(natsCluster *natsv1alpha2.NatsCluster) {
		natsCluster.Spec.Auth = &natsv1alpha2.AuthConfig{
			// Use service accounts (i.e. observe NatsServiceRole resources) for authentication.
			EnableServiceAccounts: true,
		}
		natsCluster.Spec.Pod = &natsv1alpha2.PodPolicy{
			// Enable configuration reloading.
			EnableConfigReload: true,
		}
		// Use a fixed, well-known name instead of a generated name so that the NatsServiceRole resources created above produce the intended effect.
		natsCluster.GenerateName = ""
		natsCluster.Name = clusterName
	})
	if err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(natsCluster); err != nil {
			t.Error(err)

		}
	}()

	// Wait for the single pod to be created.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx1, natsCluster, size, version); err != nil {
		t.Fatal(err)
	}

	// Connect to the NATS cluster using nsr1's bound token.
	if c, err = f.ConnectToNatsClusterWithNatsServiceRole(natsCluster, nsr1); err != nil {
		t.Fatal(err)
	}
	// Make sure nsr1 CANNOT subscribe to the "hello.world" subject.
	_, err = c.Subscribe(subject, func(msg *nats.Msg) {
		t.Logf("received message on subject %q", msg.Subject)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Flush(); err != nil {
		t.Fatal(err)
	}
	if err = c.LastError(); err == nil || !strings.Contains(err.Error(), "permissions violation for subscription") {
		t.Fatalf("expected subscription to %q to have failed", subject)
	}
	c.Close()

	// Add the "hello.world" subject to the list of allowed subjects for nsr1.
	nsr1.Spec.Permissions.Subscribe = append(nsr1.Spec.Permissions.Subscribe, subject)
	if nsr1, err = f.PatchNatsServiceRole(nsr1); err != nil {
		t.Fatal(err)
	}

	// Wait for the "Reloaded: authorization users" log message to appear in the logs for the very first pod.
	ctx2, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilPodLogLineMatches(ctx2, natsCluster, 1, "Reloaded: authorization users"); err != nil {
		t.Fatal(err)
	}

	// Connect to the NATS cluster using nsr1's bound token.
	c, err = f.ConnectToNatsClusterWithNatsServiceRole(natsCluster, nsr1)
	if err != nil {
		t.Fatal(err)
	}
	// Make sure nsr1 can now subscribe to the "hello.world" subject.
	_, err = c.Subscribe(subject, func(msg *nats.Msg) {
		t.Logf("received message on subject %q", msg.Subject)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Flush(); err != nil {
		t.Fatal(err)
	}
	if err = c.LastError(); err != nil {
		t.Fatal(err)
	}
	c.Close()
}
