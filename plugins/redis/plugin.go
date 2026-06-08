// Package redis is the vendor plugin for Redis server logs.
// Modern Redis log format (6+): {pid}:{role} {DD Mon YYYY HH:MM:SS.mmm} {level} {message}
// where role: M=main/master, R=replica, C=child, S=sentinel
// and level: * verbose, # warning, - debug, . debug
package redis

import (
	"regexp"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "redis"
	version    = "0.1.0"
)

var (
	// Modern Redis prefix: "1:M 15 Jan 2024 10:23:41.123 # message"
	redisPrefixRe = regexp.MustCompile(`^\d+:[MRCS] \d{2} \w{3} \d{4} \d{2}:\d{2}:\d{2}\.\d{3} [*#\-.]`)

	maxmemoryRe      = regexp.MustCompile(`(?i)maxmemory`)
	bgsaveFailRe     = regexp.MustCompile(`(?i)(?:Can't save in background|Background saving error|Bgsave error)`)
	replicaSyncRe    = regexp.MustCompile(`MASTER <-> REPLICA|REPLICAOF|replica synchronization|replica timeout`)
	oomKillRe        = regexp.MustCompile(`(?i)Out of memory|OOM|killed.*process`)
	replicaConnectRe = regexp.MustCompile(`(?i)Connecting to MASTER|MASTER-REPLICA|master is not reachable`)
	policyRe         = regexp.MustCompile(`(?i)(?:eviction policy|maxmemory-policy)[:\s]+(\S+)`)
	aofRewriteFailRe = regexp.MustCompile(`(?i)(?:Background append only file rewriting|AOF rewrite) (?:failed|error)`)
	// Ready to accept connections is the last line of startup for Redis 6+ and Valkey.
	startupRe = regexp.MustCompile(`Ready to accept connections`)
	// Shutdown is signalled by SIGTERM handling or an explicit shutdown command.
	shutdownRe = regexp.MustCompile(`(?i)(?:User requested shutdown|SIGTERM.*shutd|scheduling shutdown|shutting down|bye bye)`)
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// Modern Redis log prefix pattern is highly distinctive.
		{Type: plugin.RuleTypeRegex, Value: `^\d+:[MRCS] \d{2} \w{3} \d{4}`, Weight: 0.9},
		// Replication events are Redis-specific phrase pairs.
		{Type: plugin.RuleTypeSubstring, Value: "MASTER <-> REPLICA", Weight: 0.9},
		{Type: plugin.RuleTypeSubstring, Value: "Can't save in background", Weight: 0.9},
		{Type: plugin.RuleTypeSubstring, Value: "maxmemory", Weight: 0.6},
		{Type: plugin.RuleTypeSubstring, Value: "DB loaded from disk", Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: "Background saving started", Weight: 0.6},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	body := line.Body
	ts := line.Timestamp

	switch {
	case startupRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "started",
			Timestamp:   ts,
			Confidence:  0.90,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case shutdownRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "shutdown",
			Timestamp:   ts,
			Confidence:  0.88,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case maxmemoryRe.MatchString(body):
		policy := ""
		if m := policyRe.FindStringSubmatch(body); len(m) > 1 {
			policy = m[1]
		}
		newVal := "eviction_active"
		if policy != "" {
			newVal = "eviction_active:" + policy
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    newVal,
			Timestamp:   ts,
			Confidence:  0.85,
			RawLine:     body,
			TTLSeconds:  900,
		}, nil

	case bgsaveFailRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "bgsave_failed",
			Timestamp:   ts,
			Confidence:  0.92,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case aofRewriteFailRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "aof_rewrite_failed",
			Timestamp:   ts,
			Confidence:  0.88,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case replicaConnectRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "replica_master_unreachable",
			Timestamp:   ts,
			Confidence:  0.88,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case replicaSyncRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "replica_sync",
			Timestamp:   ts,
			Confidence:  0.82,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case oomKillRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "oom_killed",
			Timestamp:   ts,
			Confidence:  0.90,
			RawLine:     body,
			TTLSeconds:  1200,
		}, nil
	}

	return nil, nil
}
