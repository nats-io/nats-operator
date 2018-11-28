// natsconf is a package for producing NATS config programmatically.
package natsconf

import (
	"bytes"
	"encoding/json"
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
	Username           string       `json:"username,omitempty"`
	Password           string       `json:"password,omitempty"`
	Token              string       `json:"token,omitempty"`
	Timeout            int          `json:"timeout,omitempty"`
	Users              []*User      `json:"users,omitempty"`
	DefaultPermissions *Permissions `json:"default_permissions,omitempty"`
}

type User struct {
	User        string       `json:"username,omitempty"`
	Password    string       `json:"password,omitempty"`
	Permissions *Permissions `json:"permissions,omitempty"`
}

// Permissions are the allowed subjects on a per
// publish or subscribe basis.
type Permissions struct {
	Publish   []string `json:"publish,omitempty"`
	Subscribe []string `json:"subscribe,omitempty"`
}

// Marshal takes a server configuration and returns its
// JSON representation in bytes.
func Marshal(conf *ServerConfig) ([]byte, error) {
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(conf)
	if err != nil {
		return nil, err
	}
	buf2 := &bytes.Buffer{}
	err = json.Indent(buf2, buf.Bytes(), "", "  ")
	if err != nil {
		return nil, err
	}

	return buf2.Bytes(), nil
}

// Unmarshal attempts to parse the specified byte array as JSON as a ServerConfig object.
func Unmarshal(conf []byte) (*ServerConfig, error) {
	res := &ServerConfig{}
	if err := json.Unmarshal(conf, res); err != nil {
		return nil, err
	}
	return res, nil
}
