<div align="center">
  <img src="owl.png" alt="Noctuary" width="100" />
  <h1>Noctuary Plugins</h1>
  <p>Vendor log plugins for the <a href="https://noctuary.io">Noctuary Agent</a> — pattern matching that runs on your own hardware.</p>
  <p>
    <a href="https://github.com/noctuary-io/noctuary-plugins/actions"><img alt="Tests" src="https://github.com/noctuary-io/noctuary-plugins/actions/workflows/test.yml/badge.svg" /></a>
    <a href="https://pkg.go.dev/github.com/noctuary-io/noctuary-plugins"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/noctuary-io/noctuary-plugins.svg" /></a>
    <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-blue" /></a>
  </p>
</div>

---

## What this is

The Noctuary Agent watches your infrastructure logs — from your OTel Collector — and extracts meaningful signals: deploys, restarts, saturation events, dependency failures. It does all of this locally. Raw log lines never leave your network.

This repository contains the **plugin contract** and every **official vendor plugin**. Each plugin is a small, focused Go package that knows how to:

1. **Fingerprint** — quickly decide whether a log line looks like it came from a particular vendor
2. **Match** — extract a structured `ContextEvent` from lines that do

Plugins are compiled directly into the agent binary. No runtime config, no sidecar, no plugin server.

---

## Plugin catalogue

### Deployment & orchestration

| Plugin | Detects |
|---|---|
| [**argocd**](plugins/argocd/) | Sync operations, health transitions, rollbacks, app deletions |
| [**kubernetes**](plugins/kubernetes/) | Pod restarts, OOMKill, CrashLoopBackOff, node pressure, scale events |

### Databases

| Plugin | Detects |
|---|---|
| [**postgres**](plugins/postgres/) | Slow queries, autovacuum, connection exhaustion, crash recovery, schema changes |
| [**redis**](plugins/redis/) | Memory eviction, bgsave failure, AOF rewrite errors, replica sync failures, OOM kills |

### Message queues

| Plugin | Detects |
|---|---|
| [**kafka**](plugins/kafka/) | Broker elections, under-replicated partitions, consumer lag, ISR changes |

### Feature flags

| Plugin | Detects |
|---|---|
| [**flagd**](plugins/flagd/) | Flag evaluations, configuration reloads, resolver errors |

### Logging frameworks

| Plugin | Vendor |
|---|---|
| [**pino**](plugins/pino/) | Node.js services using [pino](https://github.com/pinojs/pino) structured JSON logging |
| [**log4j2**](plugins/log4j2/) | JVM services using [Log4j 2](https://logging.apache.org/log4j/2.x/) |
| [**gojson**](plugins/gojson/) | Go services emitting structured JSON (`level`, `msg`, `time`) |

### Catch-all

| Plugin | Notes |
|---|---|
| [**generic**](plugins/generic/) | Keyword heuristics for common error patterns — used when no vendor plugin matches |

---

## Getting an API key

Sign up at **[app.noctuary.io](https://app.noctuary.io)** and create a free account. Once logged in, go to **Settings → Agent & API Keys** and generate a key. It will look like:

```
nct_k1_xxxxxxxxxxxxxxxxxxxxxxxx
```

Keep it handy — you'll pass it to the installer or set it in `agent.yaml`. The agent runs in `stdout` mode without a key (useful for local testing), but you need one to send events to the Noctuary backend.

---

## Installing the agent

Plugins ship as part of the Noctuary Agent binary. Install the agent and all plugins are included.

### Bare metal (Linux)

Download the binary from the [latest GitHub release](https://github.com/noctuary-io/noctuary-plugins/releases/latest):

```bash
curl -sSL https://github.com/noctuary-io/noctuary-plugins/releases/latest/download/noctuary-agent-linux-amd64 \
  -o noctuary-agent
chmod +x noctuary-agent
sudo mv noctuary-agent /usr/local/bin/
```

Create a config directory and write a minimal `agent.yaml`:

```bash
sudo mkdir -p /etc/noctuary
sudo tee /etc/noctuary/agent.yaml > /dev/null <<EOF
auth:
  api_key: "nct_k1_your_key_here"

upstream:
  destination: http
  endpoint: "https://ingest.noctuary.io"

telemetry:
  otlp:
    http_listen: ":4318"
EOF
```

Register and start a systemd service:

```bash
sudo tee /etc/systemd/system/noctuary-agent.service > /dev/null <<EOF
[Unit]
Description=Noctuary Agent
After=network-online.target

[Service]
ExecStart=/usr/local/bin/noctuary-agent -config /etc/noctuary/agent.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now noctuary-agent
```

```bash
systemctl status noctuary-agent
journalctl -u noctuary-agent -f
```

### Docker

```bash
docker run --rm \
  -p 4318:4318 \
  -v /path/to/agent.yaml:/etc/noctuary/agent.yaml:ro \
  noctuary/agent:latest
```

### Kubernetes (Helm)

```bash
helm repo add noctuary https://charts.noctuary.io
helm install noctuary-agent noctuary/noctuary-agent \
  --set auth.apiKey.secretValue="nct_k1_your_key"
```

---

## Configuring plugins

By default all plugins are enabled. Disable any you don't need in `agent.yaml`:

```yaml
plugins:
  argocd:
    enabled: true
  kubernetes:
    enabled: true
  postgres:
    enabled: true
  redis:
    enabled: false   # not in this environment
  kafka:
    enabled: false
  flagd:
    enabled: true
  pino:
    enabled: true
  log4j2:
    enabled: false
  gojson:
    enabled: true
  generic:
    enabled: true
```

Restart the agent after any config change:

```bash
sudo systemctl restart noctuary-agent   # systemd
docker compose restart agent            # Docker Compose
```

---

## Connecting your OTel Collector

The agent listens on port `4318` for OTLP/HTTP/JSON. The Collector sends logs to it; the agent pattern-matches them and emits events.

The key requirement is that each log source has a `service.name` resource attribute so the agent knows which service a line came from. The right way to set this depends on how you're running your Collector.

### One Collector per host (most common)

A single Collector on a host typically reads from several log files. Use a named `filelog` receiver per service, each stamping its own `service.name`:

```yaml
receivers:
  filelog/postgres:
    include: [/var/log/postgresql/*.log]
    resource:
      service.name: postgres-primary

  filelog/redis:
    include: [/var/log/redis/*.log]
    resource:
      service.name: redis-main

  filelog/kafka:
    include: [/opt/kafka/logs/server.log]
    resource:
      service.name: kafka-broker-1

exporters:
  otlphttp/noctuary:
    endpoint: http://localhost:4318
    tls:
      insecure: true
    encoding: json      # required
    compression: none

service:
  pipelines:
    logs/postgres:
      receivers: [filelog/postgres]
      exporters: [otlphttp/noctuary]
    logs/redis:
      receivers: [filelog/redis]
      exporters: [otlphttp/noctuary]
    logs/kafka:
      receivers: [filelog/kafka]
      exporters: [otlphttp/noctuary]
```

### One Collector per service (sidecar pattern)

If each service has its own Collector, a single `resource` processor with a static value is enough:

```yaml
receivers:
  filelog:
    include: [/var/log/postgresql/*.log]

processors:
  resource:
    attributes:
      - action: insert
        key: service.name
        value: postgres-primary

exporters:
  otlphttp/noctuary:
    endpoint: http://localhost:4318
    tls:
      insecure: true
    encoding: json
    compression: none

service:
  pipelines:
    logs:
      receivers: [filelog]
      processors: [resource]
      exporters: [otlphttp/noctuary]
```

> **Note:** `encoding: json` is required. Requires OTel Collector Contrib v0.104.0 or later.

---

## Writing your own plugin

Copy `_template/myplugin/` into `plugins/<yourvendor>/` and implement two methods:

```go
// Fingerprints returns cheap rules that route log lines to your plugin.
func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
    return []plugin.FingerprintRule{
        {Type: plugin.RuleTypeSubstring, Value: "my-distinctive-string", Weight: 0.9},
        {Type: plugin.RuleTypeRegex,     Value: `^\[MyVendor\]`,         Weight: 0.8},
    }
}

// Match extracts a ContextEvent from a line that passed fingerprinting.
// Return (nil, nil) for lines that are from this vendor but not a meaningful event.
func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
    if strings.Contains(line.Body, "started") {
        return &schema.ContextEvent{
            EventType:  schema.EventTypeRestart,
            Vendor:     "myvendor",
            NewValue:   "started",
            Timestamp:  line.Timestamp,
            Confidence: 0.9,
            RawLine:    line.Body,
            TTLSeconds: 1800,
        }, nil
    }
    return nil, nil
}
```

Build and test your plugin:

```bash
go test ./plugins/myvendor/
make myvendor   # builds myvendor.wasm
```

To wire it into the agent, register it in the processor's `internal/pipeline/pipeline.go` and add it to the default config. See the [contribution guide](_template/myplugin/plugin.go) in the template for the full contract.

---

## Repository layout

```
noctuary-plugins/
├── plugin/              # VendorPlugin interface + FingerprintRule types
├── schema/              # ParsedOTelLog + ContextEvent types
├── plugins/
│   ├── argocd/
│   ├── flagd/
│   ├── generic/
│   ├── gojson/
│   ├── kafka/
│   ├── kubernetes/
│   ├── log4j2/
│   ├── pino/
│   ├── postgres/
│   └── redis/
│       ├── plugin.go        # fingerprinting + match logic
│       ├── plugin_test.go   # table-driven tests with real log lines
│       └── wasm/main.go     # WASM entry point (optional distribution format)
└── _template/myplugin/  # scaffold for a new plugin
```

Each plugin is a self-contained Go package. The `plugin/` and `schema/` packages are the only shared dependencies — no frameworks, no code generation.

---

## Building WASM binaries

Plugins can also be compiled to WebAssembly for sandboxed distribution:

```bash
make all        # build WASM for every plugin
make redis      # build a single plugin
make test       # run all tests
make clean      # remove compiled .wasm files
```

Requires Go 1.24+ with `GOOS=wasip1 GOARCH=wasm` support.

---

<div align="center">
  <sub>Built with care by the <a href="https://noctuary.io">Noctuary</a> team.</sub>
</div>
