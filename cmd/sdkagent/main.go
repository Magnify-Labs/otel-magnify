// sdkagent is a minimal OpAMP client that simulates an SDK-instrumented service.
// Used for local development and demo purposes only.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
	"google.golang.org/protobuf/proto"
)

type remoteConfigState struct {
	mu        sync.RWMutex
	effective *protobufs.AgentConfigMap
}

func (s *remoteConfigState) apply(remote *protobufs.AgentRemoteConfig) (*protobufs.RemoteConfigStatus, error) {
	if remote == nil || len(remote.ConfigHash) == 0 {
		return nil, errors.New("remote config hash is required")
	}
	if remote.Config == nil {
		return nil, errors.New("remote config content is required")
	}

	effective := proto.Clone(remote.Config).(*protobufs.AgentConfigMap)
	s.mu.Lock()
	s.effective = effective
	s.mu.Unlock()

	return &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: bytes.Clone(remote.ConfigHash),
		Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
	}, nil
}

func (s *remoteConfigState) effectiveConfig(_ context.Context) (*protobufs.EffectiveConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.effective == nil {
		return nil, nil
	}
	return &protobufs.EffectiveConfig{
		ConfigMap: proto.Clone(s.effective).(*protobufs.AgentConfigMap),
	}, nil
}

func main() {
	name := flag.String("name", "my-sdk-service", "Service name reported to OpAMP server")
	version := flag.String("version", "1.0.0", "Service version")
	env := flag.String("env", "dev", "Deployment environment label")
	endpoint := flag.String("endpoint", "ws://localhost:4320/v1/opamp", "OpAMP server WebSocket endpoint")
	acceptRemoteConfig := flag.Bool("accept-remote-config", false, "Accept remote config for local activation testing")
	flag.Parse()

	logger := log.New(os.Stdout, "["+*name+"] ", log.LstdFlags)

	opampClient := client.NewWebSocket(nil)

	if err := opampClient.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			kv("service.name", *name),
			kv("service.version", *version),
		},
		NonIdentifyingAttributes: []*protobufs.KeyValue{
			kv("deployment.environment", *env),
		},
	}); err != nil {
		logger.Fatalf("SetAgentDescription: %v", err)
	}

	capabilities := agentCapabilities(*acceptRemoteConfig)
	if err := opampClient.SetCapabilities(&capabilities); err != nil {
		logger.Fatalf("SetCapabilities: %v", err)
	}

	var uid types.InstanceUid
	if _, err := rand.Read(uid[:]); err != nil {
		logger.Fatalf("generate instance uid: %v", err)
	}

	remoteState := &remoteConfigState{}
	settings := types.StartSettings{
		OpAMPServerURL: *endpoint,
		InstanceUid:    uid,
		Callbacks: types.Callbacks{
			GetEffectiveConfig: remoteState.effectiveConfig,
			OnConnect: func(_ context.Context) {
				logger.Printf("connected to %s", *endpoint)
			},
			OnConnectFailed: func(_ context.Context, err error) {
				logger.Printf("connection failed: %v", err)
			},
			OnError: func(_ context.Context, err *protobufs.ServerErrorResponse) {
				logger.Printf("server error: %v", err.GetErrorMessage())
			},
			OnMessage: func(ctx context.Context, msg *types.MessageData) {
				if !*acceptRemoteConfig || msg == nil || msg.RemoteConfig == nil {
					return
				}
				status, err := remoteState.apply(msg.RemoteConfig)
				if err != nil {
					logger.Printf("remote config rejected: %v", err)
					return
				}
				if err := opampClient.SetRemoteConfigStatus(status); err != nil {
					logger.Printf("report remote config status: %v", err)
					return
				}
				if err := opampClient.UpdateEffectiveConfig(ctx); err != nil {
					logger.Printf("report effective config: %v", err)
					return
				}
				logger.Printf("remote config applied")
			},
		},
	}

	if err := opampClient.Start(context.Background(), settings); err != nil {
		logger.Fatalf("Start: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	logger.Println("shutting down...")
	if err := opampClient.Stop(context.Background()); err != nil {
		logger.Printf("Stop: %v", err)
	}
}

func agentCapabilities(acceptRemoteConfig bool) protobufs.AgentCapabilities {
	capabilities := protobufs.AgentCapabilities_AgentCapabilities_ReportsStatus
	if acceptRemoteConfig {
		capabilities |= protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig |
			protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig |
			protobufs.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig
	}
	return capabilities
}

func kv(key, val string) *protobufs.KeyValue {
	return &protobufs.KeyValue{
		Key: key,
		Value: &protobufs.AnyValue{
			Value: &protobufs.AnyValue_StringValue{StringValue: val},
		},
	}
}
