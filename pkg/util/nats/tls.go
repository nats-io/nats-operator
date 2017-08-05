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

package nats

import (
	"crypto/tls"
	"io/ioutil"
	"os"
	"path/filepath"
)

const (
	CliCertFile = "nats-client.crt"
	CliKeyFile  = "nats-client.key"
	CliCAFile   = "nats-client-ca.crt"
)

// TODO implement?
func NewTLSConfig(certData, keyData, caData []byte) (*tls.Config, error) {
	dir, err := ioutil.TempDir("", "nats-operator-cluster-tls")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	certFile, err := writeFile(dir, CliCertFile, certData)
	if err != nil {
		return nil, err
	}
	keyFile, err := writeFile(dir, CliKeyFile, keyData)
	if err != nil {
		return nil, err
	}
	caFile, err := writeFile(dir, CliCAFile, caData)
	if err != nil {
		return nil, err
	}

	// remove after we implement
	var _ = []string{certFile, keyFile, caFile}

	return &tls.Config{}, nil
}

func writeFile(dir, file string, data []byte) (string, error) {
	p := filepath.Join(dir, file)
	return p, ioutil.WriteFile(p, data, 0600)
}
