// Package flagd is the vendor plugin for flagd feature flag service logs.
//
// Two ingestion paths are supported:
//
//  1. OTel SDK path (primary): flagd uses the OTel zap bridge. The log body is
//     plain text (e.g. "flag evaluation") and the structured fields (flagKey,
//     variant, defaultVariant) arrive as OTel attributes. Fingerprinted via
//     RuleTypeAttributeKey so the router scores this plugin correctly.
//
//  2. Raw JSON path (fallback): when logs are scraped via the filelog receiver,
//     the full zap JSON appears in the body. Body-text fingerprints handle this.
//
// The key invariant in both paths: only emit an event when variant != defaultVariant.
// The load generator fires constant evaluations — default-variant lines are noise.
package flagd

import (
	"encoding/json"
	"strings"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "flagd"
	version    = "0.1.0"
)

// flagdBodyJSON is the raw zap JSON layout written when flagd logs via file.
type flagdBodyJSON struct {
	Level          string `json:"level"`
	Msg            string `json:"msg"`
	FlagKey        string `json:"flagKey"`
	Variant        string `json:"variant"`
	DefaultVariant string `json:"defaultVariant"`
	Reason         string `json:"reason"`
}

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// OTel SDK path — flagKey arrives as an attribute, not in the body.
		{Type: plugin.RuleTypeAttributeKey, Value: "flagKey", Weight: 0.9},
		{Type: plugin.RuleTypeAttributeKey, Value: "defaultVariant", Weight: 0.7},
		// Raw JSON path (filelog receiver) — flagKey appears in the body text.
		{Type: plugin.RuleTypeSubstring, Value: `"flagKey"`, Weight: 0.9},
		{Type: plugin.RuleTypeSubstring, Value: `"defaultVariant"`, Weight: 0.8},
		// filelog/sidecar path — service.name stamped by transform/sidecar-service.
		{Type: plugin.RuleTypeServiceName, Value: "flagd", Weight: 0.9},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	// Primary: OTel SDK — structured fields in attributes.
	if flagKey := line.Attributes["flagKey"]; flagKey != "" {
		return matchFromAttributes(line, flagKey)
	}
	// Fallback: raw JSON in body (filelog receiver / docker logs scraper).
	return matchFromBodyJSON(line)
}

func matchFromAttributes(line schema.ParsedOTelLog, flagKey string) (*schema.ContextEvent, error) {
	variant := line.Attributes["variant"]
	defaultVariant := line.Attributes["defaultVariant"]

	if variant != "" && defaultVariant != "" {
		if variant == defaultVariant {
			return nil, nil // suppress default-variant noise
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeFlagChange,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      flagKey,
			OldValue:    defaultVariant,
			NewValue:    variant,
			Timestamp:   line.Timestamp,
			Confidence:  0.97,
			RawLine:     line.Body,
			TTLSeconds:  1800,
		}, nil
	}

	// Attributes present but no variant pair — could be a config sync or startup event.
	return matchStartupFromLine(line)
}

func matchFromBodyJSON(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	var fl flagdBodyJSON
	if err := json.Unmarshal([]byte(line.Body), &fl); err != nil {
		return nil, nil
	}

	switch {
	case fl.FlagKey != "" && fl.Variant != "" && fl.DefaultVariant != "":
		if fl.Variant == fl.DefaultVariant {
			return nil, nil
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeFlagChange,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      fl.FlagKey,
			OldValue:    fl.DefaultVariant,
			NewValue:    fl.Variant,
			Timestamp:   line.Timestamp,
			Confidence:  0.97,
			RawLine:     line.Body,
			TTLSeconds:  1800,
		}, nil

	// flagd v0.14.x: "Data sync received for <path>" fires on startup and on every
	// config file change. Also keep legacy "config sync complete" for forward compat.
	case strings.HasPrefix(fl.Msg, "Data sync received") ||
		fl.Msg == "config sync complete" || fl.Msg == "configuration sync complete":
		return &schema.ContextEvent{
			EventType:   schema.EventTypeConfigChange,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "config_loaded",
			Timestamp:   line.Timestamp,
			Confidence:  0.88,
			RawLine:     line.Body,
			TTLSeconds:  900,
		}, nil

	// flagd v0.14.x: "flagd version: vX.Y.Z" is the first log line on every start.
	// Also keep legacy message strings for forward compat.
	case strings.Contains(fl.Msg, "flagd version") ||
		fl.Msg == "flagd started" || fl.Msg == "starting flagd" || fl.Msg == "service started":
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "started",
			Timestamp:   line.Timestamp,
			Confidence:  0.85,
			RawLine:     line.Body,
			TTLSeconds:  900,
		}, nil
	}

	return nil, nil
}

func matchStartupFromLine(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	return nil, nil
}
