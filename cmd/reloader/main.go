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
	fs.StringVar(&nconfig.PidFile, "P", "/var/run/nats/gnatsd.pid", "NATS Server Pid File")
	fs.StringVar(&nconfig.PidFile, "pid", "/var/run/nats/gnatsd.pid", "NATS Server Pid File")
	fs.StringVar(&nconfig.ConfigFile, "c", "/etc/nats/gnatsd.conf", "NATS Server Config File")
	fs.StringVar(&nconfig.ConfigFile, "config", "/etc/nats/gnatsd.conf", "NATS Server Config File")
	fs.IntVar(&nconfig.MaxRetries, "max-retries", 5, "Max attempts to trigger reload")
	fs.IntVar(&nconfig.RetryWaitSecs, "retry-wait-secs", 2, "Time to back off when reloading fails before retrying")

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

	// Signal handling.
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

		for sig := range c {
			log.Printf("Trapped \"%v\" signal\n", sig)
			switch sig {
			case syscall.SIGINT:
				log.Println("Exiting...")
				os.Exit(0)
				return
			case syscall.SIGTERM:
				r.Stop()
				return
			}
		}
	}()

	log.Printf("Starting NATS Server Reloader v%s\n", version.OperatorVersion)
	err = r.Run(context.Background())
	if err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(1)
	}
}
