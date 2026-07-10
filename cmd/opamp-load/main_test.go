package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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
			_ context.Context,
			_ string,
			_ string,
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

func TestMainWritesSummaryAndExits130OnSignal(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "opamp-load")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build opamp-load: %v\n%s", err, output)
	}

	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer listener.Close()

			accepted := make(chan net.Conn, 1)
			acceptErr := make(chan error, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					acceptErr <- err
					return
				}
				accepted <- connection
			}()

			endpoint := fmt.Sprintf("ws://%s/v1/opamp", listener.Addr())
			command := exec.Command(binaryPath,
				"--endpoint", endpoint,
				"--collectors", "1",
				"--ramp", "0s",
				"--hold", "1h",
			)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatalf("start opamp-load: %v", err)
			}

			var connection net.Conn
			select {
			case connection = <-accepted:
				defer connection.Close()
			case err := <-acceptErr:
				t.Fatalf("accept connection: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("opamp-load did not connect to the local test listener")
			}

			if err := command.Process.Signal(signal); err != nil {
				t.Fatalf("signal opamp-load: %v", err)
			}

			wait := make(chan error, 1)
			go func() {
				wait <- command.Wait()
			}()
			select {
			case err := <-wait:
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) || exitError.ExitCode() != 130 {
					t.Fatalf("opamp-load exit = %v, want status 130; stderr = %s", err, stderr.String())
				}
			case <-time.After(10 * time.Second):
				_ = command.Process.Kill()
				<-wait
				t.Fatal("opamp-load did not stop after signal")
			}

			var result summary
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("decode final summary: %v; stdout = %q; stderr = %s", err, stdout.String(), stderr.String())
			}
			if !result.Interrupted {
				t.Fatalf("summary = %#v, want interrupted run", result)
			}
		})
	}
}
