// Package myplugin is a template for writing a Noctuary vendor plugin.
// Copy this directory, rename it, update the package name and constants,
// then implement Fingerprints() and Match().
//
// To register your plugin with the agent, add it to the pipeline in the
// noctuary processor and rebuild. See CONTRIBUTING.md for details.
package myplugin

import (
	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "myplugin"
	version    = "0.1.0"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

// Fingerprints returns lightweight rules used to route log lines to this plugin.
// Rules are checked cheaply before Match() is called — keep them fast.
// Weight is a confidence contribution (0.0–1.0); total is capped at 1.0.
// The router invokes Match() when the total exceeds the configured threshold (default 0.5).
func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// Match a distinctive substring in the log body.
		{Type: plugin.RuleTypeSubstring, Value: "my-vendor-keyword", Weight: 0.9},
		// Match a regex pattern.
		{Type: plugin.RuleTypeRegex, Value: `^\[MyVendor\]`, Weight: 0.8},
	}
}

// Match extracts a ContextEvent from a log line that passed fingerprinting.
// Return (nil, nil) when the line is from this vendor but not a meaningful event.
// Return (event, nil) for events you want to surface.
// Return (nil, err) only for unexpected parse failures.
func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	body := line.Body

	// Example: detect a restart signal.
	if body == "my vendor started" {
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "started",
			Timestamp:   line.Timestamp,
			Confidence:  0.90,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil
	}

	return nil, nil
}
