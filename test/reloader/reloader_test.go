package reloadertest

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/nats-io/nats-operator/pkg/reloader"
)

func TestReloader(t *testing.T) {
	pid := os.Getpid()
	pidfile, err := ioutil.TempFile(os.TempDir(), "nats-pid-")
	if err != nil {
		t.Fatal(err)
	}

	p := fmt.Sprintf("%d", pid)
	if _, err := pidfile.WriteString(p); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(pidfile.Name())

	configfile, err := ioutil.TempFile(os.TempDir(), "nats-conf-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(configfile.Name())

	if _, err := configfile.WriteString("port = 4222"); err != nil {
		t.Fatal(err)
	}

	// Create tempfile with contents, then update it
	nconfig := &natsreloader.Config{
		PidFile:    pidfile.Name(),
		ConfigFile: configfile.Name(),
	}
	r, err := natsreloader.NewReloader(nconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	var success = false

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Signal handling.
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGHUP)

		// Success when receiving the first signal
		for range c {
			success = true
			cancel()
			return
		}
	}()

	go func() {
		for i := 0; i < 5; i++ {
			if _, err := configfile.WriteString("port = 4222"); err != nil {
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()

	err = r.Run(ctx)
	if err != nil && err != context.Canceled {
		t.Fatal(err)
	}
	if !success {
		t.Fatalf("Timed out waiting for reloading signal")
	}
}
