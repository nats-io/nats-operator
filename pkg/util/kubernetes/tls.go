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
	natsutil "github.com/pires/nats-operator/pkg/util/nats"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type TLSData struct {
	CertData []byte
	KeyData  []byte
	CAData   []byte
}

func GetTLSDataFromSecret(kubecli kubernetes.Interface, ns, se string) (*TLSData, error) {
	secret, err := kubecli.CoreV1().Secrets(ns).Get(se, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &TLSData{
		CertData: secret.Data[natsutil.CliCertFile],
		KeyData:  secret.Data[natsutil.CliKeyFile],
		CAData:   secret.Data[natsutil.CliCAFile],
	}, nil
}
