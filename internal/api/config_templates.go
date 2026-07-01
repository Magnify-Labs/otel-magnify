package api

import (
	"strings"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

var builtInConfigTemplateCreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func builtInConfigTemplates() []models.Config {
	vars := commonTemplateVariables()
	return []models.Config{
		{
			ID: "tpl-k8s-otlp-grafana", Name: "Kubernetes OTLP to Grafana Tempo/Loki/Mimir", Kind: models.ConfigKindTemplate, Status: models.ConfigStatusReady,
			Category: "kubernetes", Stack: "grafana", Description: "Collector pipeline for Kubernetes workloads exporting traces, logs, and metrics to Grafana Tempo, Loki, and Mimir.",
			Variables: vars, Tags: []string{"kubernetes", "otlp", "grafana", "tempo", "loki", "mimir"}, BuiltIn: true, CreatedAt: builtInConfigTemplateCreatedAt, CreatedBy: "otel-magnify",
			Content: `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
      http:
        endpoint: ${OTLP_HTTP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: service.namespace
        value: ${RESOURCE_NAMESPACE}
        action: upsert
exporters:
  otlp/tempo:
    endpoint: ${TEMPO_ENDPOINT}
    headers:
      Authorization: ${OTLP_AUTH_HEADER}
    tls:
      insecure: ${TLS_INSECURE}
  loki:
    endpoint: ${LOKI_ENDPOINT}
    headers:
      Authorization: ${LOKI_BASIC_AUTH}
    tls:
      insecure: ${TLS_INSECURE}
  prometheusremotewrite/mimir:
    endpoint: ${MIMIR_REMOTE_WRITE_ENDPOINT}
    headers:
      Authorization: ${OTLP_AUTH_HEADER}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [otlp/tempo]
    logs:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [loki]
    metrics:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [prometheusremotewrite/mimir]
`,
		},
		{
			ID: "tpl-k8s-otlp-datadog", Name: "Kubernetes OTLP to Datadog", Kind: models.ConfigKindTemplate, Status: models.ConfigStatusReady,
			Category: "kubernetes", Stack: "datadog", Description: "Collector pipeline for Kubernetes workloads exporting traces, logs, and metrics to Datadog through OTLP.",
			Variables: vars, Tags: []string{"kubernetes", "otlp", "datadog"}, BuiltIn: true, CreatedAt: builtInConfigTemplateCreatedAt, CreatedBy: "otel-magnify",
			Content: `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
      http:
        endpoint: ${OTLP_HTTP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: service.namespace
        value: ${RESOURCE_NAMESPACE}
        action: upsert
exporters:
  datadog:
    api:
      site: ${DATADOG_SITE}
      key: ${DATADOG_API_KEY}
    traces:
      endpoint: ${DATADOG_TRACE_ENDPOINT}
    metrics:
      endpoint: ${DATADOG_METRICS_ENDPOINT}
    logs:
      endpoint: ${DATADOG_LOGS_ENDPOINT}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [datadog]
    logs:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [datadog]
    metrics:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [datadog]
`,
		},
		templateConfig("tpl-logs-loki", "Logs to Loki", "logs", "loki", "Collect OTLP logs and export them to Grafana Loki.", []string{"logs", "loki", "otlp"}, `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: service.namespace
        value: ${RESOURCE_NAMESPACE}
        action: upsert
exporters:
  loki:
    endpoint: ${LOKI_ENDPOINT}
    headers:
      Authorization: ${LOKI_BASIC_AUTH}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [loki]
`),
		templateConfig("tpl-traces-tempo", "Traces to Tempo", "traces", "tempo", "Collect OTLP traces and export them to Grafana Tempo.", []string{"traces", "tempo", "otlp"}, `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: service.namespace
        value: ${RESOURCE_NAMESPACE}
        action: upsert
exporters:
  otlp/tempo:
    endpoint: ${TEMPO_ENDPOINT}
    headers:
      Authorization: ${OTLP_AUTH_HEADER}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [otlp/tempo]
`),
		templateConfig("tpl-metrics-prometheus-remote", "Metrics to Prometheus remote write", "metrics", "prometheus", "Collect OTLP metrics and export them through Prometheus remote write.", []string{"metrics", "prometheus", "remote-write", "otlp"}, `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: service.namespace
        value: ${RESOURCE_NAMESPACE}
        action: upsert
exporters:
  prometheusremotewrite:
    endpoint: ${PROMETHEUS_REMOTE_WRITE_ENDPOINT}
    headers:
      Authorization: ${OTLP_AUTH_HEADER}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [prometheusremotewrite]
`),
		templateConfig("tpl-jvm-services", "JVM services", "services", "jvm", "OTLP receiver tuned for JVM service traces, metrics, and logs.", []string{"jvm", "java", "services", "otlp"}, `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: telemetry.sdk.language
        value: java
        action: upsert
      - key: service.namespace
        value: ${RESOURCE_NAMESPACE}
        action: upsert
exporters:
  otlp:
    endpoint: ${OTLP_EXPORT_ENDPOINT}
    headers:
      Authorization: ${OTLP_AUTH_HEADER}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [otlp]
    metrics:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [otlp]
    logs:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [otlp]
`),
		templateConfig("tpl-nginx", "NGINX", "services", "nginx", "Collect NGINX access logs and metrics, then export through OTLP.", []string{"nginx", "logs", "metrics"}, `receivers:
  filelog/nginx:
    include: [${NGINX_ACCESS_LOG_PATH}]
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: service.name
        value: nginx
        action: upsert
exporters:
  otlp:
    endpoint: ${OTLP_EXPORT_ENDPOINT}
    headers:
      Authorization: ${OTLP_AUTH_HEADER}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    logs:
      receivers: [filelog/nginx]
      processors: [resource, batch]
      exporters: [otlp]
    metrics:
      receivers: [otlp]
      processors: [resource, batch]
      exporters: [otlp]
`),
		templateConfig("tpl-postgresql", "PostgreSQL", "databases", "postgresql", "Collect PostgreSQL receiver metrics and export through OTLP.", []string{"postgresql", "database", "metrics"}, `receivers:
  postgresql:
    endpoint: ${POSTGRESQL_ENDPOINT}
    username: ${POSTGRESQL_USERNAME}
    password: ${POSTGRESQL_PASSWORD}
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: service.namespace
        value: ${RESOURCE_NAMESPACE}
        action: upsert
exporters:
  otlp:
    endpoint: ${OTLP_EXPORT_ENDPOINT}
    headers:
      Authorization: ${OTLP_AUTH_HEADER}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    metrics:
      receivers: [postgresql, otlp]
      processors: [resource, batch]
      exporters: [otlp]
`),
		templateConfig("tpl-redis", "Redis", "databases", "redis", "Collect Redis receiver metrics and export through OTLP.", []string{"redis", "database", "metrics"}, `receivers:
  redis:
    endpoint: ${REDIS_ENDPOINT}
    password: ${REDIS_PASSWORD}
  otlp:
    protocols:
      grpc:
        endpoint: ${OTLP_RECEIVER_ENDPOINT}
processors:
  batch: {}
  resource:
    attributes:
      - key: deployment.environment
        value: ${ENVIRONMENT}
        action: upsert
      - key: service.namespace
        value: ${RESOURCE_NAMESPACE}
        action: upsert
exporters:
  otlp:
    endpoint: ${OTLP_EXPORT_ENDPOINT}
    headers:
      Authorization: ${OTLP_AUTH_HEADER}
    tls:
      insecure: ${TLS_INSECURE}
service:
  pipelines:
    metrics:
      receivers: [redis, otlp]
      processors: [resource, batch]
      exporters: [otlp]
`),
	}
}

func commonTemplateVariables() []models.ConfigVariable {
	return []models.ConfigVariable{
		{Name: "endpoint", Label: "Endpoint", Type: "string", Required: true, Description: "Primary exporter or receiver endpoint", Placeholder: "https://example:4317"},
		{Name: "headers", Label: "Headers", Type: "map", Required: false, Description: "Authentication or routing headers; use secret placeholders only", Placeholder: "Authorization: ${OTLP_AUTH_HEADER}"},
		{Name: "environment", Label: "Environment", Type: "string", Required: true, Description: "deployment.environment resource attribute", Placeholder: "production"},
		{Name: "resource_attributes", Label: "Resource attributes", Type: "map", Required: false, Description: "Additional resource attributes added by the resource processor", Placeholder: "service.namespace: ${RESOURCE_NAMESPACE}"},
		{Name: "tls", Label: "TLS", Type: "object", Required: false, Description: "TLS settings for exporters", Placeholder: "insecure: ${TLS_INSECURE}"},
	}
}

func templateConfig(id, name, category, stack, description string, tags []string, content string) models.Config {
	return models.Config{
		ID: id, Name: name, Content: content, CreatedAt: builtInConfigTemplateCreatedAt, CreatedBy: "otel-magnify",
		Kind: models.ConfigKindTemplate, Status: models.ConfigStatusReady, Category: category, Stack: stack, Description: description,
		Variables: commonTemplateVariables(), Tags: tags, BuiltIn: true,
	}
}

func findBuiltInConfigTemplate(id string) (models.Config, bool) {
	for _, cfg := range builtInConfigTemplates() {
		if cfg.ID == id {
			return cfg, true
		}
	}
	return models.Config{}, false
}

func configMatchesLibraryFilters(cfg models.Config, kind, category, stack string) bool {
	if kind != "" && !strings.EqualFold(cfg.Kind, kind) {
		return false
	}
	if category != "" && !strings.EqualFold(cfg.Category, category) {
		return false
	}
	if stack != "" && !strings.EqualFold(cfg.Stack, stack) {
		return false
	}
	return true
}
