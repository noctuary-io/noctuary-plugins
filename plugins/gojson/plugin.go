// Package gojson is the vendor plugin for Go structured JSON logs.
// Covers zap and slog, used by Checkout Service, Product Catalog, and Accounting Service.
// zap:  {"level":"error","ts":1705314221.123,"caller":"...","msg":"...","error":"..."}
// slog: {"time":"2024-01-15T10:23:41Z","level":"ERROR","source":{...},"msg":"...","error":"..."}
package gojson

import (
	"encoding/json"
	"strings"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "gojson"
	version    = "0.1.0"
)

// goLogLine covers the union of zap and slog field names.
type goLogLine struct {
	Level      string `json:"level"`
	Msg        string `json:"msg"`
	Error      string `json:"error"`
	Err        string `json:"err"`
	Stacktrace string `json:"stacktrace"`
}

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// ERROR-level lines — zap (lowercase) and slog (uppercase).
		{Type: plugin.RuleTypeRegex, Value: `"level":"(?:error|ERROR|FATAL|fatal)"`, Weight: 0.6},
		// "error" field is common to both zap and slog structured errors.
		{Type: plugin.RuleTypeSubstring, Value: `"error":`, Weight: 0.5},
		// "stacktrace" is a zap field present on panics and explicit zap.Stack() calls.
		{Type: plugin.RuleTypeSubstring, Value: `"stacktrace":`, Weight: 0.6},
		// "msg" is present in both zap and slog — low weight, used for score accumulation.
		{Type: plugin.RuleTypeSubstring, Value: `"msg":`, Weight: 0.3},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	var gl goLogLine
	if err := json.Unmarshal([]byte(line.Body), &gl); err != nil {
		return nil, nil
	}

	level := strings.ToUpper(gl.Level)
	isError := level == "ERROR"
	isFatal := level == "FATAL" || level == "DPANIC" || level == "PANIC"

	// Require either: ERROR/FATAL level with an error field, or a stacktrace field.
	errMsg := gl.Error
	if errMsg == "" {
		errMsg = gl.Err
	}
	hasError := errMsg != ""
	hasStack := gl.Stacktrace != ""

	if !isFatal && !(isError && (hasError || hasStack)) {
		return nil, nil
	}

	entity := line.ServiceName

	if isFatal || hasStack {
		newVal := truncate(gl.Msg, 80)
		if hasStack {
			newVal = "panic: " + newVal
		} else {
			newVal = "fatal: " + newVal
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      entity,
			NewValue:    newVal,
			Timestamp:   line.Timestamp,
			Confidence:  0.88,
			RawLine:     line.Body,
			TTLSeconds:  1800,
		}, nil
	}

	// Checkout-specific: payment charge failure is high-confidence signal.
	if strings.Contains(gl.Msg, "failed to charge") || strings.Contains(gl.Msg, "charge failed") {
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      entity,
			NewValue:    "payment_charge_failed: " + truncate(errMsg, 60),
			Timestamp:   line.Timestamp,
			Confidence:  0.93,
			RawLine:     line.Body,
			TTLSeconds:  1200,
		}, nil
	}

	return &schema.ContextEvent{
		EventType:   schema.EventTypeDependencyFailure,
		Vendor:      vendorName,
		ServiceName: line.ServiceName,
		Entity:      entity,
		NewValue:    truncate(errMsg, 80),
		Timestamp:   line.Timestamp,
		Confidence:  0.82,
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
