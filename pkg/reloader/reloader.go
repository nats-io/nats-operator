package natsreloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Config represents the configuration of the reloader.
type Config struct {
	PidFile       string
	ConfigFiles   []string
	MaxRetries    int
	RetryWaitSecs int
}

// Reloader monitors the state from a single server config file
// and sends signal on updates.
type Reloader struct {
	*Config

	// proc represents the NATS Server process which will
	// be signaled.
	proc *os.Process

	// pid is the last known PID from the NATS Server.
	pid int

	// quit shutsdown the reloader.
	quit func()
}

// Run starts the main loop.
func (r *Reloader) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.quit = func() {
		cancel()
	}

	var (
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

	configWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer configWatcher.Close()

	// Follow configuration updates in the directory where
	// the config file is located and trigger reload when
	// it is either recreated or written into.
	for i := range r.ConfigFiles {
		// Ensure our paths are canonical
		r.ConfigFiles[i], _ = filepath.Abs(r.ConfigFiles[i])
		// Use directory here because k8s remounts the entire folder
		// the config file lives in. So, watch the folder so we properly receive events.
		if err := configWatcher.Add(filepath.Dir(r.ConfigFiles[i])); err != nil {
			return err
		}
	}

	attempts = 0
	// lastConfigAppliedCache is the last config update
	// applied by us
	lastConfigAppliedCache := make(map[string][]byte)

	// Preload config hashes, so we know their digests
	// up front and avoid potentially reloading when unnecessary.
	for _, configFile := range r.ConfigFiles {
		h := sha256.New()
		f, err := os.Open(configFile)
		if err != nil {
			return err
		}
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		digest := h.Sum(nil)
		lastConfigAppliedCache[configFile] = digest
	}

WaitForEvent:
	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-configWatcher.Events:
			log.Printf("Event: %+v \n", event)
			touchedInfo, err := os.Stat(event.Name)
			if err != nil {
				continue
			}

			for _, configFile := range r.ConfigFiles {
				configInfo, err := os.Stat(configFile)
				if err != nil {
					log.Printf("Error: %s\n", err)
					continue WaitForEvent
				}
				if !os.SameFile(touchedInfo, configInfo) {
					continue
				}

				h := sha256.New()
				f, err := os.Open(configFile)
				if err != nil {
					log.Printf("Error: %s\n", err)
					continue WaitForEvent
				}
				if _, err := io.Copy(h, f); err != nil {
					log.Printf("Error: %s\n", err)
					continue WaitForEvent
				}
				digest := h.Sum(nil)
				lastConfigHash, ok := lastConfigAppliedCache[configFile]
				if ok && bytes.Equal(lastConfigHash, digest) {
					// No meaningful change or this is the first time we've checked
					continue WaitForEvent
				}
				lastConfigAppliedCache[configFile] = digest

				// We only get an event for one file at a time, we can stop checking
				// config files here and continue with our business.
				break
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
