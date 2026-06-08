// Package kubernetes matches Kubernetes API Event objects produced by the OTel
// k8sobjects receiver. Events are JSON-native so this plugin is a typed
// deserialiser and reason-code classifier, not a regex pattern matcher.
package kubernetes

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

// k8sEvent is the subset of a Kubernetes Event object that this plugin reads.
// The k8sobjects OTel receiver emits the full object as the log body.
type k8sEvent struct {
	Kind    string `json:"kind"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Type    string `json:"type"` // "Normal" or "Warning"
	Count   int    `json:"count"`

	InvolvedObject struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"involvedObject"`

	LastTimestamp string `json:"lastTimestamp"`
}

var (
	// "Scaled up replica set foo from 3 to 5" or "Scaled down ... from 5 to 3"
	replicaRangeRe = regexp.MustCompile(`from (\d+) to (\d+)`)
	// "New size: 5; reason: cpu resource..."
	newSizeRe = regexp.MustCompile(`New size:\s*(\d+)`)
	// "MemoryLimit: 512Mi" — must have a colon to distinguish from "memory limit reached"
	memLimitRe = regexp.MustCompile(`(?i)MemoryLimit:\s*(\S+)`)
	// "exit code N" in crash messages
	exitCodeRe = regexp.MustCompile(`exit code[:\s]+(\d+)`)
	// "insufficient memory" or "insufficient cpu" in FailedScheduling messages
	insufficientMemRe = regexp.MustCompile(`(?i)insufficient memory`)
	insufficientCPURe = regexp.MustCompile(`(?i)insufficient cpu`)
)

// Plugin is the Kubernetes Events vendor plugin.
type Plugin struct{}

// New returns a ready-to-use Plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return "kubernetes" }
func (p *Plugin) Version() string { return "0.1.0" }

// Fingerprints returns rules that distinguish Kubernetes event JSON from other
// log sources. "involvedObject" is present in every k8s Event object and is
// the primary anchor. Specific reason codes provide high-confidence boosts.
func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		{Type: plugin.RuleTypeSubstring, Value: `"involvedObject"`, Weight: 0.8},
		{Type: plugin.RuleTypeSubstring, Value: `OOMKilling`, Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: `CrashLoopBackOff`, Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: `ScalingReplicaSet`, Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: `SuccessfulRescale`, Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: `FailedScheduling`, Weight: 0.6},
		{Type: plugin.RuleTypeSubstring, Value: `"Evicted"`, Weight: 0.6},
	}
}

// Match attempts to parse line.Body as a Kubernetes Event JSON object and
// returns a ContextEvent for reason codes that signal meaningful state changes.
// Returns (nil, nil) when the body is not a k8s event or the reason is not
// one this plugin tracks.
func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	var ev k8sEvent
	if err := json.Unmarshal([]byte(line.Body), &ev); err != nil {
		return nil, nil
	}
	if ev.Reason == "" || ev.InvolvedObject.Name == "" {
		return nil, nil
	}
	return classify(ev, line), nil
}

func classify(ev k8sEvent, line schema.ParsedOTelLog) *schema.ContextEvent {
	obj := ev.InvolvedObject

	svcName := line.ServiceName
	if svcName == "" {
		svcName = fmt.Sprintf("%s/%s", obj.Namespace, obj.Name)
	}

	ts := ev.LastTimestamp
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}

	switch {
	case isOOM(ev):
		limit := ""
		if m := memLimitRe.FindStringSubmatch(ev.Message); len(m) > 1 {
			limit = "limit=" + m[1]
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      "kubernetes",
			ServiceName: svcName,
			Entity:      obj.Name,
			NewValue:    "OOMKilled",
			OldValue:    limit,
			Timestamp:   ts,
			Confidence:  0.95,
			RawLine:     line.Body,
			TTLSeconds:  1200,
		}

	case isCrashLoop(ev):
		exitCode := ""
		if m := exitCodeRe.FindStringSubmatch(ev.Message); len(m) > 1 {
			exitCode = "exit_code=" + m[1]
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      "kubernetes",
			ServiceName: svcName,
			Entity:      obj.Name,
			NewValue:    "CrashLoopBackOff",
			OldValue:    exitCode,
			Timestamp:   ts,
			Confidence:  0.90,
			RawLine:     line.Body,
			TTLSeconds:  1200,
		}

	case isScale(ev):
		oldVal, newVal := extractReplicaChange(ev)
		return &schema.ContextEvent{
			EventType:   schema.EventTypeScaleEvent,
			Vendor:      "kubernetes",
			ServiceName: svcName,
			Entity:      obj.Name,
			OldValue:    oldVal,
			NewValue:    newVal,
			Timestamp:   ts,
			Confidence:  0.90,
			RawLine:     line.Body,
			TTLSeconds:  1800,
		}

	case isNodePressure(ev):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      "kubernetes",
			ServiceName: svcName,
			Entity:      obj.Name,
			NewValue:    ev.Reason,
			Timestamp:   ts,
			Confidence:  0.85,
			RawLine:     line.Body,
			TTLSeconds:  2700,
		}

	case isEvicted(ev):
		// Pod evicted by kubelet due to node resource pressure. Different from
		// OOMKilling (OS-level kill) — eviction is a graceful kubelet-initiated
		// removal to reclaim node resources before the OS kills processes.
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      "kubernetes",
			ServiceName: svcName,
			Entity:      obj.Name,
			NewValue:    "Evicted",
			OldValue:    extractEvictionReason(ev),
			Timestamp:   ts,
			Confidence:  0.88,
			RawLine:     line.Body,
			TTLSeconds:  1200,
		}

	case isProbeFailure(ev):
		// Liveness probe failure → kubelet kills and restarts the container.
		// Readiness probe failure → pod removed from Service endpoints (no restart).
		// Both signal the app is not responding correctly to health checks.
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      "kubernetes",
			ServiceName: svcName,
			Entity:      obj.Name,
			NewValue:    "probe_failure",
			OldValue:    extractProbeType(ev),
			Timestamp:   ts,
			Confidence:  0.85,
			RawLine:     line.Body,
			TTLSeconds:  900,
		}

	case isSchedulingFailure(ev):
		// Pod cannot be placed on any node. The message contains the reason:
		// "0/3 nodes available: insufficient memory" → cluster capacity crunch.
		// This is a saturation event — the cluster cannot absorb more load.
		constraint := extractSchedulingConstraint(ev)
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      "kubernetes",
			ServiceName: svcName,
			Entity:      obj.Name,
			NewValue:    "FailedScheduling",
			OldValue:    constraint,
			Timestamp:   ts,
			Confidence:  0.87,
			RawLine:     line.Body,
			TTLSeconds:  1800,
		}
	}

	return nil
}

func isOOM(ev k8sEvent) bool {
	return ev.Reason == "OOMKilling" || ev.Reason == "OOMKilled"
}

// isCrashLoop detects BackOff events where the pod container is restarting
// repeatedly. Image pull backoffs share the same reason code but have different
// message content, so the message is checked.
func isCrashLoop(ev k8sEvent) bool {
	if ev.Reason != "BackOff" {
		return false
	}
	msg := strings.ToLower(ev.Message)
	return strings.Contains(msg, "restarting") || strings.Contains(msg, "crashloop")
}

func isScale(ev k8sEvent) bool {
	switch ev.Reason {
	case "ScalingReplicaSet", "SuccessfulRescale", "DesiredReplicas":
		return true
	}
	return false
}

func isNodePressure(ev k8sEvent) bool {
	switch ev.Reason {
	case "NodeNotReady", "NodeMemoryPressure", "NodeDiskPressure", "EvictionThresholdMet":
		return true
	}
	return ev.InvolvedObject.Kind == "Node" && ev.Type == "Warning"
}

func isEvicted(ev k8sEvent) bool {
	return ev.Reason == "Evicted"
}

func isProbeFailure(ev k8sEvent) bool {
	return ev.Reason == "Unhealthy"
}

func isSchedulingFailure(ev k8sEvent) bool {
	return ev.Reason == "FailedScheduling"
}

func extractReplicaChange(ev k8sEvent) (old, new_ string) {
	if m := replicaRangeRe.FindStringSubmatch(ev.Message); len(m) > 2 {
		return m[1], m[2]
	}
	if m := newSizeRe.FindStringSubmatch(ev.Message); len(m) > 1 {
		return "", m[1]
	}
	return "", ""
}

func extractEvictionReason(ev k8sEvent) string {
	msg := strings.ToLower(ev.Message)
	switch {
	case strings.Contains(msg, "memory"):
		return "memory_pressure"
	case strings.Contains(msg, "disk") || strings.Contains(msg, "ephemeral"):
		return "disk_pressure"
	case strings.Contains(msg, "pid"):
		return "pid_pressure"
	default:
		return ""
	}
}

func extractProbeType(ev k8sEvent) string {
	msg := strings.ToLower(ev.Message)
	switch {
	case strings.Contains(msg, "liveness"):
		return "liveness"
	case strings.Contains(msg, "readiness"):
		return "readiness"
	case strings.Contains(msg, "startup"):
		return "startup"
	default:
		return ""
	}
}

func extractSchedulingConstraint(ev k8sEvent) string {
	switch {
	case insufficientMemRe.MatchString(ev.Message):
		return "insufficient_memory"
	case insufficientCPURe.MatchString(ev.Message):
		return "insufficient_cpu"
	default:
		return ""
	}
}
