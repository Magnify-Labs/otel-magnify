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
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
)

const connectTimeout = 30 * time.Second

type config struct {
	endpoint   string
	collectors int
	ramp       time.Duration
	hold       time.Duration
}

type summary struct {
	Attempted    uint64 `json:"attempted"`
	Connected    uint64 `json:"connected"`
	Failed       uint64 `json:"failed"`
	Disconnected uint64 `json:"disconnected"`
}

type counters struct {
	attempted    atomic.Uint64
	connected    atomic.Uint64
	failed       atomic.Uint64
	disconnected atomic.Uint64
}

func main() {
	config, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	result := run(context.Background(), config, os.Getenv("OPAMP_SHARED_SECRET"))
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "write summary:", err)
		os.Exit(1)
	}
	if result.Failed != 0 || result.Connected != result.Attempted || result.Disconnected != result.Connected {
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

func run(ctx context.Context, config config, sharedSecret string) summary {
	var counters counters
	var ready sync.WaitGroup
	var workers sync.WaitGroup
	stop := make(chan struct{})
	startedAt := time.Now()

	for index := 0; index < config.collectors; index++ {
		counters.attempted.Add(1)
		ready.Add(1)
		workers.Add(1)
		go runCollector(ctx, config.endpoint, sharedSecret, stop, &ready, &workers, &counters)

		if config.ramp == 0 {
			continue
		}
		nextStart := startedAt.Add(time.Duration(index+1) * config.ramp / time.Duration(config.collectors))
		if delay := time.Until(nextStart); delay > 0 {
			time.Sleep(delay)
		}
	}

	ready.Wait()
	time.Sleep(config.hold)
	close(stop)
	workers.Wait()

	return summary{
		Attempted:    counters.attempted.Load(),
		Connected:    counters.connected.Load(),
		Failed:       counters.failed.Load(),
		Disconnected: counters.disconnected.Load(),
	}
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

	opampClient := client.NewWebSocket(nil)
	if err := opampClient.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			keyValue("service.name", "otelcol-load-test"),
			keyValue("service.version", "load-test"),
		},
	}); err != nil {
		counters.failed.Add(1)
		ready.Done()
		return
	}

	var instanceUID types.InstanceUid
	if _, err := rand.Read(instanceUID[:]); err != nil {
		counters.failed.Add(1)
		ready.Done()
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
		counters.failed.Add(1)
		ready.Done()
		return
	}

	connected := false
	select {
	case connected = <-connectResult:
	case <-time.After(connectTimeout):
	}
	if !connected {
		counters.failed.Add(1)
		ready.Done()
		_ = opampClient.Stop(context.Background())
		return
	}

	counters.connected.Add(1)
	ready.Done()
	<-stop
	if err := opampClient.Stop(context.Background()); err != nil {
		counters.failed.Add(1)
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
