package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats-operator/pkg/reloader"
	"github.com/nats-io/nats-operator/version"
)

func main() {
	fs := flag.NewFlagSet("nats-server-config-reloader", flag.ExitOnError)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nats-server-config-reloader [options...]\n\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	// Help and version
	var (
		showHelp    bool
		showVersion bool
	)
	fs.BoolVar(&showHelp, "h", false, "Show help")
	fs.BoolVar(&showHelp, "help", false, "Show help")
	fs.BoolVar(&showVersion, "v", false, "Show version")
	fs.BoolVar(&showVersion, "version", false, "Show version")

	nconfig := &natsreloader.Config{}
	fs.StringVar(&nconfig.PidFile, "pid", "/tmp/nats.pid", "NATS Server Pid File")
	fs.StringVar(&nconfig.ConfigFile, "config", "/tmp/nats.conf", "NATS Server Config File")
	// RetryInterval

	fs.Parse(os.Args[1:])

	switch {
	case showHelp:
		flag.Usage()
		os.Exit(0)
	case showVersion:
		fmt.Fprintf(os.Stderr, "NATS Server Config Reloader v%s\n", version.OperatorVersion)
		os.Exit(0)
	}
	r, err := natsreloader.NewReloader(nconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// Top level context which if canceled stops the main loop.
	ctx := context.Background()

	// Signal handling.
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

		for sig := range c {
			log.Printf("Trapped \"%v\" signal\n", sig)
			select {
			case <-ctx.Done():
				continue
			default:
			}

			switch sig {
			case syscall.SIGINT:
				log.Println("Exiting...\n")
				os.Exit(0)
				return
			case syscall.SIGTERM:
				r.Stop()
				return
			}
		}
	}()

	// Run until the context is canceled when quit is called.
	log.Printf("Starting NATS Server Reloader v%s\n", version.OperatorVersion)
	err = r.Run(ctx)
	if err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(1)
	}
}
