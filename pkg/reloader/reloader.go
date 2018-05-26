package natsreloader

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Config represents the configuration of the reloader.
type Config struct {
	PidFile    string
	ConfigFile string
}

// Reloader monitors the state from a single server config file
// and sends signal on updates.
type Reloader struct {
	*Config
	proc *os.Process
	pid  int
	quit func()
}

// Run starts the main loop.
func (r *Reloader) Run(ctx context.Context) error {
	configWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer configWatcher.Close()

	// Follow configuration updates
	if err := configWatcher.Add(r.ConfigFile); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	r.quit = func() {
		cancel()
	}

	for {
		var event fsnotify.Event

		select {
		case <-ctx.Done():
			return nil
		case event = <-configWatcher.Events:
			if event.Name != r.ConfigFile || event.Op != fsnotify.Write {
				continue
			}

			// The first time try to capture the process pid.
			if r.proc == nil {
				pidfile, err := ioutil.ReadFile(r.PidFile)
				if err != nil {
					return err
				}

				pid, err := strconv.Atoi(string(pidfile))
				if err != nil {
					return err
				}
				r.pid = pid

				proc, err := os.FindProcess(r.pid)
				if err != nil {
					return err
				}
				r.proc = proc
			}

		case err := <-configWatcher.Errors:
			log.Printf("Error: %s\n", err)
			continue
		}

		// Configuration was updated, try to do reload for a few times
		// otherwise give up and wait for next event.
		attempts := 0
		maxAttempts := 5
	TryReload:
		for {
			log.Println("Sending signal to server to reload configuration")
			err := r.proc.Signal(syscall.SIGHUP)
			if err != nil {
				log.Printf("Error during reload: %s\n", err)
				if attempts > maxAttempts {
					log.Println("Too many errors attempting to signal server to reload")
					return err
				}
				log.Println("Wait and retrying after some time...")
				time.Sleep(2 * time.Second)
				attempts++
				continue TryReload
			}
			break TryReload
		}
	}

	return nil
}

// Stop shutsdown the process.
func (r *Reloader) Stop() error {
	log.Println("Shutting down...")
	r.quit()
	return nil
}

// NewReloader returns a configured NATS server reloader.
func NewReloader(config *Config) (*Reloader, error) {
	return &Reloader{
		Config: config,
	}, nil
}
