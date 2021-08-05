// Copyright 2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bootconfig

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	log "github.com/sirupsen/logrus"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
	k8srestapi "k8s.io/client-go/rest"
	k8sclientcmd "k8s.io/client-go/tools/clientcmd"
)

const Version = "0.5.2"

type Options struct {
	// TargetTag is the tag that will be looked up to find
	// the public ip from the node.
	TargetTag string

	// ClientAdvertiseFileName is the name of the file where
	// the advertise configuration will be written into.
	ClientAdvertiseFileName string

	// GatewayAdvertiseFileName is the name of the file where
	// the advertise configuration will be written into for gateways.
	GatewayAdvertiseFileName string

	// NoSignals marks whether to enable the signal handler.
	NoSignals bool
}

// Controller for the boot config.
type Controller struct {
	// Start/Stop cancellation.
	quit func()

	// Client to interact with Kubernetes resources.
	kc k8sclient.Interface

	// opts is the set of options.
	opts *Options
}

// NewController creates a new controller with the configuration.
func NewController(opts *Options) *Controller {
	return &Controller{
		opts: opts,
	}
}

// SetupClients takes the configuration and prepares the rest
// clients that will be used to interact with the cluster objects.
func (c *Controller) SetupClients(cfg *k8srestapi.Config) error {
	kc, err := k8sclient.NewForConfig(cfg)
	if err != nil {
		return err
	}
	c.kc = kc
	return nil
}

// Run starts the controller.
func (c *Controller) Run(ctx context.Context) error {
	var err error
	var cfg *k8srestapi.Config
	if kubeconfig := os.Getenv("KUBERNETES_CONFIG_FILE"); kubeconfig != "" {
		cfg, err = k8sclientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = k8srestapi.InClusterConfig()
	}
	if err != nil {
		return err
	}
	if err := c.SetupClients(cfg); err != nil {
		return err
	}

	nodeName := os.Getenv("KUBERNETES_NODE_NAME")
	if nodeName == "" {
		return errors.New("Target node name is missing")
	}
	log.Infof("Pod running on node %q", nodeName)

	node, err := c.kc.CoreV1().Nodes().Get(nodeName, k8smetav1.GetOptions{})
	if err != nil {
		return err
	}

	var ok bool
	var externalAddress string
	for _, addr := range node.Status.Addresses {
		if addr.Type == "ExternalIP" {
			externalAddress = addr.Address
			ok = true
			break
		}
  // Fallback to use the InternalIP if no external is defined
		if addr.Type == "InternalIP" {
			externalAddress = addr.Address
			ok = true
		}
	}
	// Fallback to use a label to find the external address.
	if !ok {
		externalAddress, ok = node.Labels[c.opts.TargetTag]
		if !ok || len(externalAddress) == 0 {
			return errors.New("Could not find external IP address.")
		}
	}
	log.Infof("Pod is running on node with external IP: %s", externalAddress)

	clientAdvertiseConfig := fmt.Sprintf("\nclient_advertise = \"%s\"\n\n", externalAddress)

	err = ioutil.WriteFile(c.opts.ClientAdvertiseFileName, []byte(clientAdvertiseConfig), 0644)
	if err != nil {
		return fmt.Errorf("Could not write client advertise config: %s", err)
	}
	log.Infof("Successfully wrote client advertise config to %q", c.opts.ClientAdvertiseFileName)

	gatewayAdvertiseConfig := fmt.Sprintf("\nadvertise = \"%s\"\n\n", externalAddress)

	err = ioutil.WriteFile(c.opts.GatewayAdvertiseFileName, []byte(gatewayAdvertiseConfig), 0644)
	if err != nil {
		return fmt.Errorf("Could not write gateway advertise config: %s", err)
	}
	log.Infof("Successfully wrote gateway advertise config to %q", c.opts.GatewayAdvertiseFileName)

	return nil
}
