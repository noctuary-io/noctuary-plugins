// Package argocd is the vendor plugin for ArgoCD application controller logs.
// ArgoCD uses logrus structured logging with key=value pairs.
// Fingerprints: presence of dest-server= field, "Sync operation", "App health changed".
package argocd

import (
	"regexp"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "argocd"
	version    = "0.1.0"
)

var (
	syncStartRe = regexp.MustCompile(`msg="Sync operation starting"`)
	syncSuccRe  = regexp.MustCompile(`msg="Sync operation succeeded"`)
	syncFailRe  = regexp.MustCompile(`msg="Sync operation failed"`)
	healthRe    = regexp.MustCompile(`msg="App health changed"`)
	autoSyncRe  = regexp.MustCompile(`msg="Triggering automatic sync"`)
	repoFailRe  = regexp.MustCompile(`msg="Failed to get git client for repo"`)

	// kvRe extracts logrus-format key=value and key="quoted value" pairs.
	kvRe = regexp.MustCompile(`([\w-]+)=(?:"([^"]*)"|(\S+))`)
)

// Plugin implements plugin.VendorPlugin for ArgoCD.
type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// dest-server= is unique to ArgoCD — no other common tool uses this field name.
		{Type: plugin.RuleTypeFieldName, Value: "dest-server", Weight: 0.9},
		// "Sync operation" appears in sync start/success/fail lines.
		{Type: plugin.RuleTypeSubstring, Value: "Sync operation", Weight: 0.7},
		// "App health changed" appears in health transition lines.
		{Type: plugin.RuleTypeSubstring, Value: "App health changed", Weight: 0.7},
		// "Triggering automatic sync" appears in auto-sync lines.
		{Type: plugin.RuleTypeSubstring, Value: "Triggering automatic sync", Weight: 0.6},
		// "Failed to get git client for repo" is the ArgoCD repo connectivity error.
		{Type: plugin.RuleTypeSubstring, Value: "Failed to get git client for repo", Weight: 0.8},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	body := line.Body

	fields := parseKV(body)
	app := fields["app"]
	if app == "" {
		return nil, nil
	}

	ts := fields["time"]
	if ts == "" {
		ts = line.Timestamp
	}

	switch {
	case syncStartRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDeploy,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      app,
			NewValue:    "syncing:" + fields["revision"],
			SHA:         fields["revision"],
			Timestamp:   ts,
			Confidence:  0.95,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case syncSuccRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDeploy,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      app,
			NewValue:    "succeeded",
			SHA:         fields["revision"],
			Timestamp:   ts,
			Confidence:  0.97,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case syncFailRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDeploy,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      app,
			NewValue:    "failed",
			SHA:         fields["revision"],
			Timestamp:   ts,
			Confidence:  0.97,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case healthRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDeploy,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      app,
			OldValue:    fields["from"],
			NewValue:    "health:" + fields["to"],
			Timestamp:   ts,
			Confidence:  0.93,
			RawLine:     body,
			TTLSeconds:  900,
		}, nil

	case autoSyncRe.MatchString(body):
		// Auto-sync is triggered by the image updater or automated CI pipeline.
		// Distinct from a manual sync — the LLM should know whether a human or
		// automation deployed the revision.
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDeploy,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      app,
			NewValue:    "auto_syncing:" + fields["revision"],
			SHA:         fields["revision"],
			Timestamp:   ts,
			Confidence:  0.93,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case repoFailRe.MatchString(body):
		// ArgoCD cannot reach the git repository — all deploys for this app are stalled
		// until the repo server recovers. Classified as dependency_failure, not deploy.
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      app,
			NewValue:    "repo_unavailable",
			Timestamp:   ts,
			Confidence:  0.92,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil
	}

	return nil, nil
}

// parseKV extracts key=value and key="quoted value" pairs from a logrus log line.
func parseKV(line string) map[string]string {
	m := make(map[string]string)
	for _, match := range kvRe.FindAllStringSubmatch(line, -1) {
		key := match[1]
		val := match[2]
		if val == "" {
			val = match[3]
		}
		m[key] = val
	}
	return m
}
