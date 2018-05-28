package natsreloader

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Config represents the configuration of the reloader.
type Config struct {
	PidFile       string
	ConfigFile    string
	MaxRetries    int
	RetryWaitSecs int
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
	ctx, cancel := context.WithCancel(ctx)
	r.quit = func() {
		cancel()
	}

	var (
		err      error
		proc     *os.Process
		pid      int
		attempts int
	)
	for {
		pidfile, err := ioutil.ReadFile(r.PidFile)
		if err != nil {
			goto WaitAndRetry
		}

		pid, err = strconv.Atoi(string(pidfile))
		if err != nil {
			goto WaitAndRetry
		}

		proc, err = os.FindProcess(pid)
		if err != nil {
			goto WaitAndRetry
		}
		break

	WaitAndRetry:
		log.Printf("Error: %s\n", err)
		attempts++
		if attempts > r.MaxRetries {
			return fmt.Errorf("Too many errors attempting to find server process")
		}
		time.Sleep(time.Duration(r.RetryWaitSecs) * time.Second)
	}
	r.pid = pid
	r.proc = proc

	var (
		event         fsnotify.Event
		configWatcher *fsnotify.Watcher
	)
	configWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer configWatcher.Close()

	// Follow configuration updates in the directory where
	// the config file is located and trigger reload when
	// it is either recreated or written into.
	if err := configWatcher.Add(path.Dir(r.ConfigFile)); err != nil {
		return err
	}

	attempts = 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case event = <-configWatcher.Events:
			log.Printf("Event: %+v\n", event)
			if event.Name != r.ConfigFile || (event.Op != fsnotify.Write && event.Op != fsnotify.Create) {
				continue
			}
		case err := <-configWatcher.Errors:
			log.Printf("Error: %s\n", err)
			continue
		}

		// Configuration was updated, try to do reload for a few times
		// otherwise give up and wait for next event.
	TryReload:
		for {
			log.Println("Sending signal to server to reload configuration")
			err := r.proc.Signal(syscall.SIGHUP)
			if err != nil {
				log.Printf("Error during reload: %s\n", err)
				if attempts > r.MaxRetries {
					return fmt.Errorf("Too many errors attempting to signal server to reload")
				}
				log.Println("Wait and retrying after some time...")
				time.Sleep(time.Duration(r.RetryWaitSecs) * time.Second)
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
