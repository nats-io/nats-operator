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

package constants

const (
	// DefaultNatsVersion is the nats server version to use.
	DefaultNatsVersion = "1.4.0"

	// ClientPort is the port for the clients.
	ClientPort = 4222

	// ClusterPort is the port for server routes.
	ClusterPort = 6222

	// MonitoringPort is the port for the server monitoring endpoint.
	MonitoringPort = 8222

	// MetricsPort is the port for the prometheus metrics endpoint.
	MetricsPort = 7777

	// ConnectRetries is the number of retries for an implicit route.
	ConnectRetries = 10

	// ConfigMapVolumeName is the name of the volume use for the shared config map.
	ConfigMapVolumeName = "nats-config"

	// ConfigMapMountPath is the path on which the shared ConfigMap
	// for the NATS cluster will be located.
	ConfigMapMountPath = "/etc/nats-config"

	// ConfigFileName is the name of the config file used by the NATS server.
	ConfigFileName = "nats.conf"

	// ConfigFilePath is the absolute path to the NATS config file.
	ConfigFilePath = ConfigMapMountPath + "/" + ConfigFileName

	// BootConfigFilePath is the path to the include file that
	// contains the external IP address.
	BootConfigFilePath = "advertise/client_advertise.conf"

	// PidFileVolumeName is the name of the volume used for the NATS server pid file.
	PidFileVolumeName = "pid"

	// PidFileName is the pid file name.
	PidFileName = "gnatsd.pid"

	// PidFileMountPath is the absolute path to the directory where NATS
	// will be leaving its pid file.
	PidFileMountPath = "/var/run/nats"

	// PidFilePath is the location of the pid file.
	PidFilePath = PidFileMountPath + "/" + PidFileName

	// ServerSecretVolumeName is the name of the volume used for the server certs.
	ServerSecretVolumeName = "server-tls-certs"

	// ServerCertsMountPath is the path where the server certificates
	// to secure clients connections are located.
	ServerCertsMountPath = "/etc/nats-server-tls-certs"
	ServerCAFilePath     = ServerCertsMountPath + "/ca.pem"
	ServerCertFilePath   = ServerCertsMountPath + "/server.pem"
	ServerKeyFilePath    = ServerCertsMountPath + "/server-key.pem"

	// RoutesSecretVolumeName is the name of the volume used for the routes certs.
	RoutesSecretVolumeName = "routes-tls-certs"

	// RoutesCertsMountPath is the path where the certificates
	// to secure routes connections are located.
	RoutesCertsMountPath = "/etc/nats-routes-tls-certs"
	RoutesCAFilePath     = RoutesCertsMountPath + "/ca.pem"
	RoutesCertFilePath   = RoutesCertsMountPath + "/route.pem"
	RoutesKeyFilePath    = RoutesCertsMountPath + "/route-key.pem"

	// Default Docker Images
	DefaultServerImage             = "nats"
	DefaultReloaderImage           = "connecteverything/nats-server-config-reloader"
	DefaultReloaderImageTag        = "0.2.2-v1alpha2"
	DefaultReloaderImagePullPolicy = "IfNotPresent"
	DefaultMetricsImage            = "synadia/prometheus-nats-exporter"
	DefaultMetricsImageTag         = "0.1.0"
	DefaultMetricsImagePullPolicy  = "IfNotPresent"

	// NatsBinaryPath is the path to the NATS binary inside the main container.
	NatsBinaryPath = "/gnatsd"
	// NatsContainerName is the name of the main container.
	NatsContainerName = "nats"

	// KubernetesNamespaceNatsIO represents the "nats-io" Kubernetes namespace.
	KubernetesNamespaceNatsIO = "nats-io"
)
