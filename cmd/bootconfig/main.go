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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/nats-io/nats-operator/pkg/bootconfig"
	log "github.com/sirupsen/logrus"
)

func main() {
	fs := flag.NewFlagSet("nats-pod-bootconfig", flag.ExitOnError)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nats-pod-bootconfig [options...]\n\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	var opts *bootconfig.Options = &bootconfig.Options{}
	var showHelp, showVersion bool
	fs.BoolVar(&showHelp, "h", false, "Show help")
	fs.BoolVar(&showVersion, "v", false, "Show version")
	fs.StringVar(&opts.ClientAdvertiseFileName, "f", "client_advertise.conf", "File name where the client advertise address will be written into")
	fs.StringVar(&opts.GatewayAdvertiseFileName, "gf", "gateway_advertise.conf", "File name where the gateway advertise address will be written into")
	fs.StringVar(&opts.TargetTag, "t", "nats.io/node-external-ip", "Tag that will be looked up from a node")
	fs.Parse(os.Args[1:])

	switch {
	case showHelp:
		flag.Usage()
		os.Exit(0)
	case showVersion:
		fmt.Fprintf(os.Stderr, "NATS Server Pod boot config v%s\n", bootconfig.Version)
		os.Exit(0)
	}
	if os.Getenv("DEBUG") == "true" {
		log.SetLevel(log.DebugLevel)
	}
	formatter := &log.TextFormatter{
		FullTimestamp: true,
	}
	log.SetFormatter(formatter)

	controller := bootconfig.NewController(opts)
	log.Infof("Starting NATS Server boot config v%s", bootconfig.Version)
	err := controller.Run(context.Background())
	if err != nil && err != context.Canceled {
		log.Fatalf(err.Error())
	}
}
