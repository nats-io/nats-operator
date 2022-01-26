package natsreloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Config represents the configuration of the reloader.
type Config struct {
	PidFile       string
	ConfigFiles   []string
	MaxRetries    int
	RetryWaitSecs int
	Signal        os.Signal
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

func (r *Reloader) waitForProcess() error {
	var proc *os.Process
	var pid int
	var attempts int = 0

	startTime := time.Now()
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

	if attempts > 0 {
		log.Printf("found pid from pidfile %q after %v failed attempts (%v time after start)",
			r.PidFile, attempts, time.Since(startTime))
	}

	r.pid = pid
	r.proc = proc
	log.Printf("Live, ready to kick pid %v", r.proc.Pid)
	return nil
}

// Run starts the main loop.
func (r *Reloader) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.quit = func() {
		cancel()
	}

	err := r.waitForProcess()
	if err != nil {
		return err
	}

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

	attempts := 0
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

	// If the two pids don't match then os.FindProcess() has done something
	// rather hinkier than we expect, but log them both just in case on some
	// future platform there's a weird namespace issue, as a difference will
	// help with debugging.
	log.Printf("Live, ready to kick pid %v (live, from %v spec) based on any of %v files",
		r.proc.Pid, r.pid, len(lastConfigAppliedCache))

	if len(lastConfigAppliedCache) == 0 {
		log.Printf("Error: no watched config files cached; input spec was: %#v",
			r.ConfigFiles)
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
				// Beware that this means that we won't reconfigure if a file
				// is permanently removed.  We want to support transient
				// disappearance, waiting for the new content, and have not set
				// up any sort of longer-term timers to detect permanent
				// deletion.
				// If you really need this, then switch a file to be empty
				// before removing if afterwards.
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

				log.Printf("changed config; file=%q existing=%v total-files=%d",
					configFile, ok, len(lastConfigAppliedCache))

				// We only get an event for one file at a time, we can stop checking
				// config files here and continue with our business.
				break
			}
		case err := <-configWatcher.Errors:
			log.Printf("Error: %s\n", err)
			continue
		}

		// Reread process data, in case it changed - just to ensure to send the signal to
		// the right process.
		err = r.waitForProcess()
		if err != nil {
			return err
		}

		// Configuration was updated, try to do reload for a few times
		// otherwise give up and wait for next event.
	TryReload:
		for {
			log.Printf("Sending signal '%s' to server to reload configuration\n", r.Signal.String())
			err := r.proc.Signal(r.Signal)
			if err != nil {
				log.Printf("Error during reload: %s\n", err)
				if attempts > r.MaxRetries {
					return fmt.Errorf("Too many errors (%v) attempting to signal server to reload", attempts)
				}
				delay := retryJitter(time.Duration(r.RetryWaitSecs) * time.Second)
				log.Printf("Wait and retrying after some time [%v] ...", delay)
				time.Sleep(delay)
				attempts++
				continue TryReload
			}
			break TryReload
		}
	}
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

// retryJitter helps avoid trying things at synchronized times, thus improving
// resiliency in aggregate.
func retryJitter(base time.Duration) time.Duration {
	b := float64(base)
	// 10% +/-
	offset := rand.Float64()*0.2 - 0.1
	return time.Duration(b + offset)
}
