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

package kubernetes

import (
	"context"
	"io/ioutil"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	executil "k8s.io/client-go/util/exec"
)

// ExecInContainer runs a command in the specified container, blocking until the command finishes execution or the specified context times out.
// It returns the command's exit code (if available), its output (stdout and stderr) and the associated error (if any).
func ExecInContainer(ctx context.Context, kubeClient kubernetes.Interface, kubeCfg *rest.Config, podNamespace, podName, containerName string, args ...string) (int, error) {
	var (
		err      error
		exitCode int
	)

	// Build the "exec" request targeting the specified pod.
	request := kubeClient.CoreV1().RESTClient().Post().Resource("pods").Namespace(podNamespace).Name(podName).SubResource("exec")
	// Target the specified container and capture stdout and stderr.
	request.VersionedParams(&v1.PodExecOptions{
		Container: containerName,
		Command:   args,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)
	// The "exec" request require a connection upgrade, so we must use a custom executor.
	executor, err := remotecommand.NewSPDYExecutor(kubeCfg, "POST", request.URL())
	if err != nil {
		return -1, err
	}

	// For the time being we can drop stdout/stderr.
	// If later on we need to capture them, we'll have to replace "ioutil.Discard" with a proper buffer.
	streamOptions := remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: ioutil.Discard,
		Stderr: ioutil.Discard,
		Tty:    false,
	}

	// Perform the request and attempt to extract the command's exit code so it can be returned.
	doneCh := make(chan struct{})
	go func() {
		if err = executor.Stream(streamOptions); err != nil {
			if ceErr, ok := err.(executil.CodeExitError); ok {
				exitCode = ceErr.Code
			} else {
				exitCode = -1
			}
		}
		// Signal that the call to "Stream()" has finished.
		close(doneCh)
	}()

	// Wait for the call to "Stream()" to finish or until the specified timeout.
	select {
	case <-doneCh:
		return exitCode, err
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}
