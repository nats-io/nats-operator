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
	"fmt"

	"github.com/nats-io/go-nats"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

// ConnectToNatsClusterWithUsernamePassword attempts to connect to the specified NATS cluster using the username and password.
// It returns the NATS connection back to the caller, or an error.
// It is the caller's responsibility to close the connection when it is no longer needed.
func (f *Framework) ConnectToNatsClusterWithUsernamePassword(natsCluster *natsv1alpha2.NatsCluster, username, password string) (*nats.Conn, error) {
	// Connect to the NATS cluster represented by the specified NatsCluster resource using the specified credentials.
	c, err := nats.Connect(fmt.Sprintf("nats://%s:%d", kubernetes.ClientServiceName(natsCluster.Name), constants.ClientPort), nats.UserInfo(username, password))
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ConnectToNatsClusterWithNatsServiceRole attempts to connect to the specified NATS cluster using the specified NatsServiceRole.
// It returns the NATS connection back to the caller, or an error.
// It is the caller's responsibility to close the connection when it is no longer needed.
func (f *Framework) ConnectToNatsClusterWithNatsServiceRole(natsCluster *natsv1alpha2.NatsCluster, nsr *natsv1alpha2.NatsServiceRole) (*nats.Conn, error) {
	// Get the name of the secret that holds the token used for authentication.
	s, err := f.KubeClient.CoreV1().Secrets(f.Namespace).Get(fmt.Sprintf("%s-%s-bound-token", nsr.Name, natsCluster.Name), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Grab the service account token from the secret.
	t := string(s.Data["token"])
	// Connect to the NATS cluster represented by the specified NatsCluster resource using the NatsServiceRole's name as the username and the its service account token as the password.
	return f.ConnectToNatsClusterWithUsernamePassword(natsCluster, nsr.Name, t)
}
