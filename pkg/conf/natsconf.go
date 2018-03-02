// natsconf is a package for producing NATS config programmatically.
package natsconf

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type ServerConfig struct {
	Host           string               `json:"host,omitempty"`
	Port           int                  `json:"port,omitempty"`
	HTTPPort       int                  `json:"http_port,omitempty"`
	Cluster        *ClusterConfig       `json:"cluster,omitempty"`
	TLS            *TLSConfig           `json:"tls,omitempty"`
	Debug          bool                 `json:"debug,omitempty"`
	Trace          bool                 `json:"trace,omitempty"`
	WriteDeadline  string               `json:"write_deadline,omitempty"`
	MaxConnections int                  `json:"max_connections,omitempty"`
	MaxPayload     int                  `json:"max_payload,omitempty"`
	Authorization  *AuthorizationConfig `json:"authorization,omitempty"`
}

type ClusterConfig struct {
	Port          int                  `json:"port,omitempty"`
	Routes        []string             `json:"routes,omitempty"`
	TLS           *TLSConfig           `json:"tls,omitempty"`
	Authorization *AuthorizationConfig `json:"authorization,omitempty"`
}

type TLSConfig struct {
	CAFile           string   `json:"ca_file,omitempty"`
	CertFile         string   `json:"cert_file,omitempty"`
	KeyFile          string   `json:"key_file,omitempty"`
	Verify           bool     `json:"verify,omitempty"`
	CipherSuites     []string `json:"cipher_suites,omitempty"`
	CurvePreferences []string `json:"curve_preferences,omitempty"`
	Timeout          float64  `json:"timeout,omitempty"`
}

type AuthorizationConfig struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	Timeout  int    `json:"timeout,omitempty"`
}

func Marshal(conf *ServerConfig) ([]byte, error) {
	js, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(js) < 1 || len(js)-1 <= 1 {
		return nil, fmt.Errorf("cannot produce valid config")
	}

	// Slice the initial and final brackets from the
	// resulting JSON configuration so gnatsd config parsers
	// almost treats it as valid config.
	js = js[1:]
	js = js[:len(js)-1]

	// Replacing all commas with line breaks still keeps
	// arrays valid and makes the top level configuration
	// be able to be parsed as gnatsd config.
	result := bytes.Replace(js, []byte(","), []byte("\n"), -1)

	return result, nil
}
