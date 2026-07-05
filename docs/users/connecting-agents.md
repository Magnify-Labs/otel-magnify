# Connecting agents

Agents connect to otel-magnify over [OpAMP](https://opentelemetry.io/docs/specs/opamp/) on port `:4320` (configurable via `OPAMP_ADDR`).

Two agent types are supported:

- **OTel Collectors** — the standard `otelcol*` binaries.
- **SDK agents** — any application using the OpenTelemetry SDK with an OpAMP client.

Agent type is detected from the `service.name` reported in the `AgentDescription` message. Anything matching the `otelcol*` pattern is treated as a Collector; everything else as an SDK agent.

## Workload identity

otel-magnify groups connected agents into **workloads** — a Kubernetes Deployment, DaemonSet, StatefulSet, Job, or CronJob for K8s-native collectors, or a single host/process for anything else. The workload is the unit you see in the inventory and the unit that carries the active configuration; individual pods are shown as *instances* of that workload.

The identity is derived from resource attributes on the OpAMP `AgentDescription`:

| Strategy | Attributes used |
|---|---|
| **k8s** | `k8s.cluster.name` (defaults to `unknown`) + `k8s.namespace.name` + one of `k8s.deployment.name` / `k8s.daemonset.name` / `k8s.statefulset.name` / `k8s.job.name` / `k8s.cronjob.name` |
| **host** | `service.name` + `host.name` |
| **uid** | fallback on the OpAMP `InstanceUid` (cardinality 1 per process) |

The first strategy that can be satisfied is used. For a Kubernetes collector, enable the `resourcedetection` processor with the `k8s` detector to populate the required attributes automatically.

### Pod lifecycle

- When a new pod of an existing workload connects, the server auto-pushes the workload's active config if the pod's effective config diverges (P.2 semantics).
- When the last pod of a workload disconnects, the workload stays `connected` for a grace window (`WORKLOAD_DISCONNECT_GRACE_SECONDS`, default 120 s). After the grace it flips to `disconnected` and starts its retention countdown (`WORKLOAD_RETENTION_DAYS`, default 30 days), at the end of which the workload is archived.
- Every pod connect, disconnect, and `service.version` change is recorded in an append-only `workload_events` log (`WORKLOAD_EVENT_RETENTION_DAYS`, default 30 days). The Activity tab on the workload detail page renders this log — a noisy churn rate is a signal of a Kubernetes problem (CrashLoopBackOff, OOMKill, eviction storms).

!!! note "Migration from `/api/agents`"
    The legacy `/api/agents/*` endpoints still resolve — they reply with HTTP `307 Temporary Redirect` to their `/api/workloads/*` equivalent. Existing integrations keep working; new integrations should call `/api/workloads/*` directly.

## Configuring an OTel Collector

Add an `opamp` extension to your Collector configuration and reference it in `service::extensions`:

```yaml
extensions:
  opamp:
    server:
      ws:
        endpoint: ws://magnify.example.com:4320/v1/opamp
    instance_uid: collector-prod-eu-01
    capabilities:
      reports_effective_config: true
      reports_remote_config: true
      reports_health: true
      accepts_remote_config: true

service:
  extensions: [opamp]
  pipelines:
    # ...
```

Sample configs are available in the repo under `agents/collector-*.yaml` — they ship with the `resourcedetection` and `resource` processors pre-wired so the collector is fingerprinted correctly out of the box.

## Running a demo Collector alongside otel-magnify

```bash
docker run -d --name collector-prod-eu --network otel-magnify_default \
  -v $(pwd)/agents/collector-prod-eu.yaml:/etc/otelcol-contrib/config.yaml \
  otel/opentelemetry-collector-contrib:0.98.0
```

## Running a Collector via OpAMP Supervisor

The Collector's built-in `opamp` extension reports status and effective config,
but **does not apply remote configs**. To enable config push, run the Collector
under the [OpAMP Supervisor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/cmd/opampsupervisor).

The supervisor is not shipped as an official Docker image, so you build it
yourself. Minimal recipe:

```dockerfile
FROM golang:1.25.11 AS build
WORKDIR /src
RUN git clone --depth=1 --branch=v0.150.0 \
    https://github.com/open-telemetry/opentelemetry-collector-contrib.git
WORKDIR /src/opentelemetry-collector-contrib/cmd/opampsupervisor
RUN CGO_ENABLED=0 go build -o /out/opampsupervisor .

FROM otel/opentelemetry-collector-contrib:0.150.1
COPY --from=build /out/opampsupervisor /usr/local/bin/opampsupervisor
ENTRYPOINT ["/usr/local/bin/opampsupervisor"]
CMD ["--config", "/etc/otelcol/supervisor.yaml"]
```

Supervisor configuration (`supervisor.yaml`):

```yaml
server:
  endpoint: ws://otel-magnify:4320/v1/opamp
  tls:
    insecure: true

capabilities:
  accepts_remote_config: true     # required for config push
  reports_effective_config: true
  reports_health: true
  reports_remote_config: true

agent:
  executable: /otelcol-contrib    # path inside the contrib image
  description:
    identifying_attributes:
      service.name: otelcol-contrib    # must match otelcol* to be classified as a collector
      service.version: 0.150.1
      service.instance.id: collector-supervised-eu
    non_identifying_attributes:
      deployment.environment: production

storage:
  directory: /tmp/supervisor       # needs a writable dir inside the container
```

Run it:

```bash
docker run -d --name collector-supervised-eu --network otel-magnify_default \
  --user 0 --tmpfs /tmp:exec \
  -v $(pwd)/supervisor.yaml:/etc/otelcol/supervisor.yaml:ro \
  otel-magnify-opampsupervisor:latest
```

`--user 0` + `--tmpfs /tmp:exec` are needed because the contrib base image is
distroless and otherwise has no writable path for the supervisor storage dir.

## Simulating an SDK agent

For development and testing, the repo ships a small simulator at `cmd/sdkagent/` that connects as a fake SDK agent.

```bash
go run ./cmd/sdkagent/ --endpoint ws://localhost:4320/v1/opamp --name demo-sdk-agent
```

## What otel-magnify captures from an agent

- Identity: `service.name`, `service.version`, `service.instance.id`, labels, plus the K8s/host resource attributes used to fingerprint the workload.
- Effective configuration (what the agent currently runs).
- Remote configuration status (was the last push applied?).
- Available components — modules compiled into the agent, used to validate pushed configs against what the agent actually supports.
- Health — reported periodically, drives the alert engine.
