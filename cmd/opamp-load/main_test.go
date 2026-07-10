package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    config
		wantErr string
	}{
		{
			name: "defaults",
			want: config{
				endpoint:   "ws://localhost:4320/v1/opamp",
				collectors: 5000,
				ramp:       5 * time.Minute,
				hold:       10 * time.Minute,
			},
		},
		{
			name: "accepts explicit values",
			args: []string{
				"--endpoint", "ws://otel-magnify:4320/v1/opamp",
				"--collectors", "5000",
				"--ramp", "1m",
				"--hold", "5m",
				"--ready-file", "/artifacts/ready.json",
			},
			want: config{
				endpoint:   "ws://otel-magnify:4320/v1/opamp",
				collectors: 5000,
				ramp:       time.Minute,
				hold:       5 * time.Minute,
				readyFile:  "/artifacts/ready.json",
			},
		},
		{name: "rejects zero collectors", args: []string{"--collectors", "0"}, wantErr: "collectors"},
		{name: "rejects negative collectors", args: []string{"--collectors", "-1"}, wantErr: "collectors"},
		{name: "rejects empty endpoint", args: []string{"--endpoint", ""}, wantErr: "endpoint"},
		{name: "rejects negative ramp", args: []string{"--ramp", "-1s"}, wantErr: "ramp"},
		{name: "rejects negative hold", args: []string{"--hold", "-1s"}, wantErr: "hold"},
		{name: "rejects malformed duration", args: []string{"--hold", "later"}, wantErr: "invalid value"},
		{name: "rejects positional arguments", args: []string{"unexpected"}, wantErr: "unexpected arguments"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseConfig(test.args)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseConfig() error = %v, want error containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("parseConfig() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRunStopsConnectedCollectorsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	result := make(chan summary, 1)
	go func() {
		result <- runWithCollector(ctx, config{collectors: 1, hold: time.Hour}, "", func(
			ctx context.Context,
			endpoint string,
			sharedSecret string,
			stop <-chan struct{},
			readyGroup *sync.WaitGroup,
			workers *sync.WaitGroup,
			counters *counters,
		) {
			defer workers.Done()
			counters.connected.Add(1)
			readyGroup.Done()
			close(ready)
			<-stop
			counters.disconnected.Add(1)
		})
	}()

	<-ready
	cancel()

	select {
	case got := <-result:
		if got.Attempted != 1 || got.Connected != 1 || got.Failed != 0 || got.Cancelled != 0 || got.Disconnected != 1 || got.StopFailed != 0 || !got.Interrupted {
			t.Fatalf("summary = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop connected collectors after cancellation")
	}
}
