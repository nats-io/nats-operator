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
	"flag"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-operator/test/e2e/framework"
)

const (
	// waitTimeout is the (default) amount of time we want to wait for certain conditions to be met.
	waitTimeout = 2 * time.Minute
)

var (
	// f is the testing framework used for running the test suite.
	f *framework.Framework

	// kubeconfig is the path to the kubeconfig file to use when running the test suite outside a Kubernetes cluster (i.e. in "wait" mode).
	kubeconfig string
	// namespace is the name of the Kubernetes namespace to use for running the test suite.
	namespace string
	// wait indicates whether we start in wait mode (i.e. instead of running the e2e test suite, connect to the kubernetes cluster and wait for the e2e job to complete).
	wait bool
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to the kubeconfig file to use (e.g. $HOME/.kube/config)")
	flag.StringVar(&namespace, "namespace", "default", "name of the kubernetes namespace to use")
	flag.BoolVar(&wait, "wait", false, "instead of running the e2e test suite, connect to the kubernetes cluster and wait for the e2e job to complete")
	flag.Parse()
}

func TestMain(m *testing.M) {
	f = framework.New(kubeconfig, namespace)
	f.WaitForNatsOperator()

	if wait {
		// Wait for the nats-operator-e2e pod to be running and start streaming logs until it terminates.
		c, err := f.WaitForNatsOperatorE2ePodTermination()
		if err != nil {
			panic(err)
		}
		// Delete the nats-operator and nats-operator-e2e pods.
		f.Cleanup()
		// Exit with the same exit code as nats-operator-e2e.
		os.Exit(c)
	} else {
		// Run the test suite.
		os.Exit(m.Run())
	}
}
