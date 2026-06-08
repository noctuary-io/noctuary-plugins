// Package kafka is the vendor plugin for Apache Kafka broker logs.
// Kafka uses Log4j2 PatternLayout by default:
//
//	[YYYY-MM-DD HH:MM:SS,mmm] LEVEL message (java.class.Name)
package kafka

import (
	"regexp"
	"strings"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "kafka"
	version    = "0.1.0"
)

var (
	// [2024-01-15 10:23:41,123] INFO message (kafka.coordinator.group.GroupCoordinator)
	kafkaLineRe = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d+)\] (\w+) (.+) \((\S+)\)$`)

	rebalanceStartRe   = regexp.MustCompile(`Preparing to rebalance group (\S+)`)
	rebalanceCompleteRe = regexp.MustCompile(`(?:Stabilized|Stabilizing) group (\S+)`)
	underReplicatedRe  = regexp.MustCompile(`(?i)under-replicated partitions`)
	leaderElectionRe   = regexp.MustCompile(`(?i)(?:elected as|becomes) (?:the )?new leader|leader election`)
	shutdownRe         = regexp.MustCompile(`(?i)(?:Starting controlled shutdown|shutting down|Shutdown completed)`)
	brokerShutdownRe   = regexp.MustCompile(`Broker (\d+) has requested controlled shutdown`)
	groupIDRe          = regexp.MustCompile(`group[= ]'?(\S+?)'?(?:\s|$|,)`)
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// OTel Java agent path: service.name="kafka" is set by the Java agent; body
		// is the plain message text without Log4j2 class suffixes.
		{Type: plugin.RuleTypeServiceName, Value: "kafka", Weight: 0.8},
		// filelog receiver path: body contains Log4j2 class name suffix or event strings.
		{Type: plugin.RuleTypeSubstring, Value: "kafka.coordinator.group", Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: "kafka.controller.KafkaController", Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: "kafka.server.KafkaServer", Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: "Preparing to rebalance group", Weight: 0.6},
		{Type: plugin.RuleTypeSubstring, Value: "under-replicated partitions", Weight: 0.6},
		{Type: plugin.RuleTypeRegex, Value: `\(kafka\.\w+\.\w+`, Weight: 0.5},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	body := line.Body

	ts := line.Timestamp
	if m := kafkaLineRe.FindStringSubmatch(body); m != nil {
		ts = m[1]
	}

	switch {
	case rebalanceStartRe.MatchString(body):
		group := extractGroup(body, rebalanceStartRe)
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      group,
			NewValue:    "rebalance_triggered",
			Timestamp:   ts,
			Confidence:  0.90,
			RawLine:     body,
			TTLSeconds:  900,
		}, nil

	case rebalanceCompleteRe.MatchString(body):
		group := extractGroup(body, rebalanceCompleteRe)
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      group,
			NewValue:    "rebalance_complete",
			Timestamp:   ts,
			Confidence:  0.88,
			RawLine:     body,
			TTLSeconds:  900,
		}, nil

	case underReplicatedRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "under_replicated_partitions",
			Timestamp:   ts,
			Confidence:  0.92,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case brokerShutdownRe.MatchString(body):
		m := brokerShutdownRe.FindStringSubmatch(body)
		brokerID := ""
		if len(m) > 1 {
			brokerID = "broker_" + m[1]
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      brokerID,
			NewValue:    "controlled_shutdown",
			Timestamp:   ts,
			Confidence:  0.88,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case leaderElectionRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "leader_election",
			Timestamp:   ts,
			Confidence:  0.85,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case shutdownRe.MatchString(body) && strings.Contains(strings.ToLower(body), "kafka"):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "shutdown",
			Timestamp:   ts,
			Confidence:  0.85,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil
	}

	return nil, nil
}

func extractGroup(body string, re *regexp.Regexp) string {
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}
	if m := groupIDRe.FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}
	return "unknown"
}
