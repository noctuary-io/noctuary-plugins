// Package pino is the vendor plugin for Node.js pino logger JSON output.
// Used by Payment Service and Frontend in the OTel demo.
// Pino uses integer log levels: 10=trace, 20=debug, 30=info, 40=warn, 50=error, 60=fatal.
// Timestamps are Unix milliseconds (13-digit integer).
//
//	{"level":50,"time":1705314221123,"msg":"...","pid":1234,"err":{"message":"...","stack":"..."}}
package pino

import (
	"encoding/json"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "pino"
	version    = "0.1.0"
)

const (
	pinoLevelError = 50
	pinoLevelFatal = 60
)

type pinoLine struct {
	Level int    `json:"level"`
	Msg   string `json:"msg"`
	PID   int    `json:"pid"`
	Err   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Stack   string `json:"stack"`
	} `json:"err"`
}

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// Raw JSON path — integer error level unique to pino.
		{Type: plugin.RuleTypeRegex, Value: `"level":(?:50|60)`, Weight: 0.7},
		// Raw JSON path — 13-digit Unix millisecond timestamp.
		{Type: plugin.RuleTypeRegex, Value: `"time":\d{13}`, Weight: 0.7},
		// Raw JSON path — pid field in every pino log.
		{Type: plugin.RuleTypeSubstring, Value: `"pid":`, Weight: 0.4},
		// OTel SDK path — payment/frontend services wrap pino via OTel bridge.
		// err.type attribute is flattened from kvlistValue by the receiver.
		{Type: plugin.RuleTypeAttributeKey, Value: "err.type", Weight: 0.8},
		{Type: plugin.RuleTypeServiceName, Value: "payment", Weight: 0.7},
		{Type: plugin.RuleTypeServiceName, Value: "frontend", Weight: 0.6},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	// OTel SDK path: payment/frontend send plain-text body + err.* attributes.
	if errType := line.Attributes["err.type"]; errType != "" {
		return p.matchFromAttributes(line, errType)
	}

	// Raw JSON path: filelog receiver delivers full pino JSON in body.
	return p.matchFromBody(line)
}

func (p *Plugin) matchFromAttributes(line schema.ParsedOTelLog, errType string) (*schema.ContextEvent, error) {
	sev := line.Severity
	isFatal := len(sev) >= 5 && (sev[:5] == "FATAL" || sev[:5] == "fatal")

	newVal := errType
	if msg := line.Attributes["err.message"]; msg != "" {
		newVal = errType + ": " + truncate(msg, 80)
	} else if line.Body != "" {
		newVal = truncate(line.Body, 80)
	}

	if isFatal {
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      line.ServiceName,
			NewValue:    "fatal: " + truncate(newVal, 80),
			Timestamp:   line.Timestamp,
			Confidence:  0.90,
			RawLine:     line.Body,
			TTLSeconds:  1800,
		}, nil
	}

	return &schema.ContextEvent{
		EventType:   schema.EventTypeDependencyFailure,
		Vendor:      vendorName,
		ServiceName: line.ServiceName,
		Entity:      line.ServiceName,
		NewValue:    truncate(newVal, 80),
		Timestamp:   line.Timestamp,
		Confidence:  0.85,
		RawLine:     line.Body,
		TTLSeconds:  1200,
	}, nil
}

func (p *Plugin) matchFromBody(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	var pl pinoLine
	if err := json.Unmarshal([]byte(line.Body), &pl); err != nil {
		return nil, nil
	}

	if pl.Level < pinoLevelError {
		return nil, nil
	}

	entity := line.ServiceName
	ts := line.Timestamp

	if pl.Level >= pinoLevelFatal {
		newVal := truncate(pl.Msg, 80)
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      entity,
			NewValue:    "fatal: " + newVal,
			Timestamp:   ts,
			Confidence:  0.92,
			RawLine:     line.Body,
			TTLSeconds:  1800,
		}, nil
	}

	newVal := ""
	if pl.Err != nil && pl.Err.Message != "" {
		newVal = pl.Err.Message
		if pl.Err.Type != "" {
			newVal = pl.Err.Type + ": " + newVal
		}
	} else {
		newVal = truncate(pl.Msg, 80)
	}

	return &schema.ContextEvent{
		EventType:   schema.EventTypeDependencyFailure,
		Vendor:      vendorName,
		ServiceName: line.ServiceName,
		Entity:      entity,
		NewValue:    truncate(newVal, 80),
		Timestamp:   ts,
		Confidence:  0.88,
		RawLine:     line.Body,
		TTLSeconds:  1200,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
