// +build e2e

// Copyright 2019 The nats-operator Authors
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
	"testing"
	"time"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	natsconf "github.com/nats-io/nats-operator/pkg/conf"
	"github.com/nats-io/nats-operator/pkg/constants"
	v1 "k8s.io/api/core/v1"
	watchapi "k8s.io/apimachinery/pkg/watch"
)

func TestCreateServerWithGateways(t *testing.T) {
	var (
		size    = 1
		version = "2.0.0"
		nc      *natsv1alpha2.NatsCluster
		err     error
	)
	nc, err = f.CreateCluster(f.Namespace, "", size, version,
		func(cluster *natsv1alpha2.NatsCluster) {
			cluster.Name = "test-nats-gw"

			cluster.Spec.ServerConfig = &natsv1alpha2.ServerConfig{
				Debug: true,
				Trace: true,
			}

			cluster.Spec.Pod = &natsv1alpha2.PodPolicy{
				AdvertiseExternalIP: true,
			}

			cluster.Spec.GatewayConfig = &natsv1alpha2.GatewayConfig{
				Name:          "minikube",
				Port:          32328,
				RejectUnknown: true,
				Gateways: []*natsv1alpha2.RemoteGatewayOpts{
					&natsv1alpha2.RemoteGatewayOpts{
						Name: "minikube",
						URL:  "nats://127.0.0.1:32328",
					},
				},
			}
			cluster.Spec.PodTemplate = &v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					ServiceAccountName: "nats-server",
				},
			}
		})
	if err != nil {
		t.Fatal(err)
	}

	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(nc); err != nil {
			t.Error(err)
		}
	}()

	ctx, done := context.WithTimeout(context.Background(), 3*time.Minute)
	defer done()
	err = f.WaitUntilSecretCondition(ctx, nc, func(event watchapi.Event) (bool, error) {
		secret := event.Object.(*v1.Secret)
		conf, ok := secret.Data[constants.ConfigFileName]
		if !ok {
			return false, nil
		}
		conf = bytes.Replace(conf, []byte(`include`), []byte(`"include": `), -1)
		config, err := natsconf.Unmarshal(conf)
		if err != nil {
			return false, nil
		}
		if !config.Debug || !config.Trace {
			return false, nil
		}
		if config.Gateway == nil {
			return false, nil
		}
		if config.Gateway.Name != "minikube" || config.Gateway.Port != 32328 || !config.Gateway.RejectUnknown {
			return false, nil
		}
		pods, err := f.PodsForNatsCluster(nc)
		if err != nil {
			return false, nil
		}
		if len(pods) < 1 {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 30; i++ {
		ctx, done = context.WithTimeout(context.Background(), waitTimeout)
		defer done()
		if err = f.WaitUntilPodBootContainerLogLineMatches(ctx, nc, 1, `advertise`); err != nil {
			t.Logf("Error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if err = f.WaitUntilPodLogLineMatches(ctx, nc, 1, `Advertise address for gateway "minikube" is set to 127.0.0.1:32328`); err != nil {
			t.Logf("Error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		t.Logf("Succeeded in finding advertise address")
		break
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateServerWithGatewayAndLeafnodes(t *testing.T) {
	var (
		size    = 1
		image   = "synadia/nats-server"
		version = "2.0.0"
		nc      *natsv1alpha2.NatsCluster
		err     error

		// FIXME: This is not end-to-end until we add an account server
		// that can be used an URL resolver, can be done in separate PR.
		systemAccount = "AASYS..."
		resolver      = "URL(https://example.com/jwt/v1/accounts/)"
		jwtSecret     = "test-operator-jwt"
	)
	nc, err = f.CreateCluster(f.Namespace, "", size, version,
		func(cluster *natsv1alpha2.NatsCluster) {
			cluster.Name = "test-nats-leaf"
			cluster.Spec.ServerImage = image

			cluster.Spec.ServerConfig = &natsv1alpha2.ServerConfig{
				Debug: true,
				Trace: true,
			}

			cluster.Spec.Pod = &natsv1alpha2.PodPolicy{
				AdvertiseExternalIP: true,
			}

			cluster.Spec.GatewayConfig = &natsv1alpha2.GatewayConfig{
				Name: "minikube",
				Port: 32329,
				Gateways: []*natsv1alpha2.RemoteGatewayOpts{
					&natsv1alpha2.RemoteGatewayOpts{
						Name: "minikube",
						URL:  "nats://127.0.0.1:32329",
					},
				},
			}
			cluster.Spec.PodTemplate = &v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					ServiceAccountName: "nats-server",
				},
			}
			cluster.Spec.OperatorConfig = &natsv1alpha2.OperatorConfig{
				Secret:        jwtSecret,
				SystemAccount: systemAccount,
				Resolver:      resolver,
			}
			cluster.Spec.LeafNodeConfig = &natsv1alpha2.LeafNodeConfig{
				Port: 4224,
			}
		})
	if err != nil {
		t.Fatal(err)
	}

	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(nc); err != nil {
			t.Error(err)
		}
	}()

	ctx, done := context.WithTimeout(context.Background(), 3*time.Minute)
	defer done()
	err = f.WaitUntilSecretCondition(ctx, nc, func(event watchapi.Event) (bool, error) {
		secret := event.Object.(*v1.Secret)
		conf, ok := secret.Data[constants.ConfigFileName]
		if !ok {
			return false, nil
		}
		conf = bytes.Replace(conf, []byte(`include`), []byte(`"include": `), -1)
		config, err := natsconf.Unmarshal(conf)
		if err != nil {
			return false, nil
		}
		if !config.Debug || !config.Trace {
			return false, nil
		}
		if config.Gateway == nil {
			return false, nil
		}
		if config.Gateway.Name != "minikube" || config.Gateway.Port != 32329 {
			return false, nil
		}
		if config.JWT != "/etc/nats-operator/op.jwt" {
			return false, nil
		}
		if config.SystemAccount != systemAccount {
			return false, nil
		}
		if config.Resolver != resolver {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
