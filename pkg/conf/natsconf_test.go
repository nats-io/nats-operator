package natsconf

import (
	"strings"
	"testing"
)

func TestConfMarshal(t *testing.T) {
	tests := []struct {
		input  *ServerConfig
		output string
		err    error
	}{
		{
			input: &ServerConfig{},
			output: `{
  "logtime": false
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				HTTPPort: 8222,
			},
			output: `{
  "http_port": 8222,
  "logtime": false
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				Port: 4222,
			},
			output: `{
  "port": 4222,
  "logtime": false
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				Port:     4222,
				HTTPPort: 8222,
			},
			output: `{
  "port": 4222,
  "http_port": 8222,
  "logtime": false
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				LameDuckDuration: "2m",
				Logtime:          true,
			},
			output: `{
  "logtime": true,
  "lame_duck_duration": "2m"
}`,
		},
		{
			input: &ServerConfig{
				Port:     4222,
				HTTPPort: 8222,
				Cluster: &ClusterConfig{
					Port: 6222,
				},
			},
			output: `{
  "port": 4222,
  "http_port": 8222,
  "cluster": {
    "port": 6222
  },
  "logtime": false
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				Port:     4222,
				HTTPPort: 8222,
				Cluster: &ClusterConfig{
					Port: 6222,
					Routes: []string{
						"nats://nats-1.default.svc:6222",
						"nats://nats-2.default.svc:6222",
						"nats://nats-3.default.svc:6222",
					},
				},
			},
			output: `{
  "port": 4222,
  "http_port": 8222,
  "cluster": {
    "port": 6222,
    "routes": [
      "nats://nats-1.default.svc:6222",
      "nats://nats-2.default.svc:6222",
      "nats://nats-3.default.svc:6222"
    ]
  },
  "logtime": false
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				Port:     4222,
				HTTPPort: 8222,
				Debug:    true,
				Trace:    true,
				Cluster: &ClusterConfig{
					Port: 6222,
					Routes: []string{
						"nats://nats-1.default.svc:6222",
						"nats://nats-2.default.svc:6222",
						"nats://nats-3.default.svc:6222",
					},
				},
			},
			output: `{
  "port": 4222,
  "http_port": 8222,
  "cluster": {
    "port": 6222,
    "routes": [
      "nats://nats-1.default.svc:6222",
      "nats://nats-2.default.svc:6222",
      "nats://nats-3.default.svc:6222"
    ]
  },
  "debug": true,
  "trace": true,
  "logtime": false
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				Port:     4222,
				HTTPPort: 8222,
				Cluster: &ClusterConfig{
					Port: 6222,
					Routes: []string{
						"nats://nats-1.default.svc:6222",
						"nats://nats-2.default.svc:6222",
						"nats://nats-3.default.svc:6222",
					},
					TLS: &TLSConfig{
						CAFile:   "/etc/nats-tls/ca.pem",
						CertFile: "/etc/nats-tls/server.pem",
						KeyFile:  "/etc/nats-tls/server-key.pem",
					},
				},
			},
			output: `{
  "port": 4222,
  "http_port": 8222,
  "cluster": {
    "port": 6222,
    "routes": [
      "nats://nats-1.default.svc:6222",
      "nats://nats-2.default.svc:6222",
      "nats://nats-3.default.svc:6222"
    ],
    "tls": {
      "ca_file": "/etc/nats-tls/ca.pem",
      "cert_file": "/etc/nats-tls/server.pem",
      "key_file": "/etc/nats-tls/server-key.pem"
    }
  },
  "logtime": false
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				Port:     4222,
				HTTPPort: 8222,
				Authorization: &AuthorizationConfig{
					DefaultPermissions: &Permissions{
						Publish:   []string{"PUBLISH.>"},
						Subscribe: []string{"PUBLISH.*"},
					},
				},
			},
			output: `{
  "port": 4222,
  "http_port": 8222,
  "logtime": false,
  "authorization": {
    "default_permissions": {
      "publish": [
        "PUBLISH.>"
      ],
      "subscribe": [
        "PUBLISH.*"
      ]
    }
  }
}`,
			err: nil,
		},
		{
			input: &ServerConfig{
				Authorization: &AuthorizationConfig{
					DefaultPermissions: &Permissions{
						Publish: map[string][]string{
							"allow": []string{"hello", "world"},
							"deny":  []string{"foo.*", "bar.>"},
						},
						Subscribe: map[string][]string{
							"allow": []string{"hi", "everyone"},
						},
					},
				},
			},
			output: `{
  "logtime": false,
  "authorization": {
    "default_permissions": {
      "publish": {
        "allow": [
          "hello",
          "world"
        ],
        "deny": [
          "foo.*",
          "bar.>"
        ]
      },
      "subscribe": {
        "allow": [
          "hi",
          "everyone"
        ]
      }
    }
  }
}`,

			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run("config", func(t *testing.T) {
			res, err := Marshal(tt.input)
			if err != nil && tt.err == nil {
				t.Errorf("Error: %s", err)
			}
			o := strings.TrimSpace(string(res))
			if o != tt.output {
				t.Errorf("Expected %+v, got: %+v", tt.output, o)
			}
		})
	}
}
