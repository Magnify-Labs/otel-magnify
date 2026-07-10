// opamp-load establishes a reproducible number of OpAMP collector connections.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
)

const (
	connectTimeout = 30 * time.Second
	stopTimeout    = 5 * time.Second
)

type config struct {
	endpoint   string
	collectors int
	ramp       time.Duration
	hold       time.Duration
	readyFile  string
}

type summary struct {
	Attempted    uint64 `json:"attempted"`
	Connected    uint64 `json:"connected"`
	Failed       uint64 `json:"failed"`
	Cancelled    uint64 `json:"cancelled"`
	Disconnected uint64 `json:"disconnected"`
	StopFailed   uint64 `json:"stop_failed"`
	Interrupted  bool   `json:"interrupted"`
}

type counters struct {
	attempted    atomic.Uint64
	connected    atomic.Uint64
	failed       atomic.Uint64
	cancelled    atomic.Uint64
	disconnected atomic.Uint64
	stopFailed   atomic.Uint64
}

type collectorFunc func(context.Context, string, string, <-chan struct{}, *sync.WaitGroup, *sync.WaitGroup, *counters)

func main() {
	config, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, runErr := run(ctx, config, os.Getenv("OPAMP_SHARED_SECRET"))
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "write summary:", err)
		os.Exit(1)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
	if result.Interrupted {
		os.Exit(130)
	}
	if result.Failed != 0 || result.Cancelled != 0 || result.StopFailed != 0 || result.Connected != result.Attempted || result.Disconnected != result.Connected {
		os.Exit(1)
	}
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("opamp-load", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	config := config{}
	flags.StringVar(&config.endpoint, "endpoint", "ws://localhost:4320/v1/opamp", "OpAMP WebSocket endpoint")
	flags.IntVar(&config.collectors, "collectors", 5000, "number of collectors to connect")
	flags.DurationVar(&config.ramp, "ramp", 5*time.Minute, "time used to start all collectors")
	flags.DurationVar(&config.hold, "hold", 10*time.Minute, "time to hold connections after they are established")
	flags.StringVar(&config.readyFile, "ready-file", "", "write a JSON summary after all collectors connect")

	if err := flags.Parse(args); err != nil {
		return config, err
	}
	if flags.NArg() != 0 {
		return config, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if config.endpoint == "" {
		return config, fmt.Errorf("endpoint must not be empty")
	}
	if config.collectors <= 0 {
		return config, fmt.Errorf("collectors must be greater than zero")
	}
	if config.ramp < 0 {
		return config, fmt.Errorf("ramp must not be negative")
	}
	if config.hold < 0 {
		return config, fmt.Errorf("hold must not be negative")
	}

	return config, nil
}

func run(ctx context.Context, config config, sharedSecret string) (summary, error) {
	return runWithReporter(ctx, config, sharedSecret, runCollector, func(ready summary) error {
		return writeSummary(config.readyFile, ready)
	})
}

func runWithCollector(ctx context.Context, config config, sharedSecret string, collector collectorFunc) summary {
	result, _ := runWithReporter(ctx, config, sharedSecret, collector, nil)
	return result
}

func runWithReporter(
	ctx context.Context,
	config config,
	sharedSecret string,
	collector collectorFunc,
	reportReady func(summary) error,
) (summary, error) {
	var counters counters
	var ready sync.WaitGroup
	var workers sync.WaitGroup
	stop := make(chan struct{})
	startedAt := time.Now()

	for index := 0; index < config.collectors; index++ {
		if ctx.Err() != nil {
			break
		}
		counters.attempted.Add(1)
		ready.Add(1)
		workers.Add(1)
		go collector(ctx, config.endpoint, sharedSecret, stop, &ready, &workers, &counters)

		if config.ramp == 0 {
			continue
		}
		nextStart := startedAt.Add(time.Duration(index+1) * config.ramp / time.Duration(config.collectors))
		if delay := time.Until(nextStart); delay > 0 {
			if !wait(ctx, delay) {
				break
			}
		}
	}

	ready.Wait()
	if ctx.Err() == nil && counters.failed.Load() == 0 && counters.cancelled.Load() == 0 {
		readySummary := snapshot(&counters, false)
		if reportReady != nil {
			if err := reportReady(readySummary); err != nil {
				close(stop)
				workers.Wait()
				return snapshot(&counters, ctx.Err() != nil), fmt.Errorf("write ready summary: %w", err)
			}
		}
		wait(ctx, config.hold)
	}
	close(stop)
	workers.Wait()

	return snapshot(&counters, ctx.Err() != nil), nil
}

func snapshot(counters *counters, interrupted bool) summary {
	return summary{
		Attempted:    counters.attempted.Load(),
		Connected:    counters.connected.Load(),
		Failed:       counters.failed.Load(),
		Cancelled:    counters.cancelled.Load(),
		Disconnected: counters.disconnected.Load(),
		StopFailed:   counters.stopFailed.Load(),
		Interrupted:  interrupted,
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func stopCollector(opampClient client.OpAMPClient) error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()

	return opampClient.Stop(ctx)
}

func writeSummary(path string, result summary) error {
	if path == "" {
		return nil
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".opamp-load-ready-*.json")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)

	if err := json.NewEncoder(file).Encode(result); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func runCollector(
	ctx context.Context,
	endpoint string,
	sharedSecret string,
	stop <-chan struct{},
	ready *sync.WaitGroup,
	workers *sync.WaitGroup,
	counters *counters,
) {
	defer workers.Done()
	var readyOnce sync.Once
	markReady := func() {
		readyOnce.Do(ready.Done)
	}
	defer markReady()

	opampClient := client.NewWebSocket(nil)
	if err := opampClient.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			keyValue("service.name", "otelcol-load-test"),
			keyValue("service.version", "load-test"),
		},
	}); err != nil {
		counters.failed.Add(1)
		return
	}

	var instanceUID types.InstanceUid
	if _, err := rand.Read(instanceUID[:]); err != nil {
		counters.failed.Add(1)
		return
	}
	if ctx.Err() != nil {
		counters.cancelled.Add(1)
		return
	}

	connectResult := make(chan bool, 1)
	var connectOnce sync.Once
	recordConnect := func(connected bool) {
		connectOnce.Do(func() {
			connectResult <- connected
		})
	}

	headers := make(http.Header)
	if sharedSecret != "" {
		headers.Set("Authorization", "Bearer "+sharedSecret)
	}
	if err := opampClient.Start(ctx, types.StartSettings{
		OpAMPServerURL: endpoint,
		Header:         headers,
		InstanceUid:    instanceUID,
		Callbacks: types.Callbacks{
			OnConnect: func(context.Context) {
				recordConnect(true)
			},
			OnConnectFailed: func(context.Context, error) {
				recordConnect(false)
			},
		},
	}); err != nil {
		if ctx.Err() != nil {
			counters.cancelled.Add(1)
		} else {
			counters.failed.Add(1)
		}
		return
	}

	connected := false
	select {
	case connected = <-connectResult:
	case <-ctx.Done():
		counters.cancelled.Add(1)
		_ = stopCollector(opampClient)
		return
	case <-time.After(connectTimeout):
	}
	if !connected {
		counters.failed.Add(1)
		_ = stopCollector(opampClient)
		return
	}

	counters.connected.Add(1)
	markReady()
	select {
	case <-stop:
	case <-ctx.Done():
	}
	if err := stopCollector(opampClient); err != nil {
		counters.stopFailed.Add(1)
		return
	}
	counters.disconnected.Add(1)
}

func keyValue(key string, value string) *protobufs.KeyValue {
	return &protobufs.KeyValue{
		Key: key,
		Value: &protobufs.AnyValue{
			Value: &protobufs.AnyValue_StringValue{StringValue: value},
		},
	}
}
