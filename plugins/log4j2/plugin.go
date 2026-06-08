// Package log4j2 is the vendor plugin for Log4j2 JSON layout logs.
// Used by JVM services in the OTel demo: Ad Service and Fraud Detection Service.
// Log4j2 JSON layout produces:
//
//	{"instant":{...},"thread":"...","level":"ERROR","loggerName":"...","message":"...","thrown":{...}}
package log4j2

import (
	"encoding/json"
	"strings"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "log4j2"
	version    = "0.1.0"
)

type log4j2Line struct {
	Level      string `json:"level"`
	LoggerName string `json:"loggerName"`
	Message    string `json:"message"`
	Thread     string `json:"thread"`
	Thrown     *struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"thrown"`
}

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// "loggerName" and "instant" are unique to Log4j2 JSON layout.
		{Type: plugin.RuleTypeSubstring, Value: `"loggerName"`, Weight: 0.7},
		{Type: plugin.RuleTypeSubstring, Value: `"instant"`, Weight: 0.6},
		// "thrown" appears in Log4j2 exception records.
		{Type: plugin.RuleTypeSubstring, Value: `"thrown"`, Weight: 0.7},
		// "threadName" is another Log4j2 JSON field.
		{Type: plugin.RuleTypeSubstring, Value: `"threadName"`, Weight: 0.5},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	var ll log4j2Line
	if err := json.Unmarshal([]byte(line.Body), &ll); err != nil {
		return nil, nil
	}

	// Gate: only fire on ERROR/FATAL or when an exception is present.
	level := strings.ToUpper(ll.Level)
	isError := level == "ERROR"
	isFatal := level == "FATAL"
	hasThrown := ll.Thrown != nil && ll.Thrown.Name != ""

	if !isError && !isFatal && !hasThrown {
		return nil, nil
	}

	entity := ll.LoggerName
	if entity == "" {
		entity = line.ServiceName
	}

	switch {
	case hasThrown:
		excClass := ll.Thrown.Name
		newVal := excClass
		if ll.Thrown.Message != "" {
			newVal = excClass + ": " + ll.Thrown.Message
		}

		if strings.Contains(excClass, "OutOfMemoryError") {
			return &schema.ContextEvent{
				EventType:   schema.EventTypeRestart,
				Vendor:      vendorName,
				ServiceName: line.ServiceName,
				Entity:      entity,
				NewValue:    "OOM: " + excClass,
				Timestamp:   line.Timestamp,
				Confidence:  0.95,
				RawLine:     line.Body,
				TTLSeconds:  1800,
			}, nil
		}

		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      entity,
			NewValue:    newVal,
			Timestamp:   line.Timestamp,
			Confidence:  0.90,
			RawLine:     line.Body,
			TTLSeconds:  1200,
		}, nil

	case isFatal:
		return &schema.ContextEvent{
			EventType:   schema.EventTypeRestart,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      entity,
			NewValue:    "fatal: " + truncate(ll.Message, 80),
			Timestamp:   line.Timestamp,
			Confidence:  0.92,
			RawLine:     line.Body,
			TTLSeconds:  1800,
		}, nil

	case isError:
		newVal := truncate(ll.Message, 80)
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      entity,
			NewValue:    newVal,
			Timestamp:   line.Timestamp,
			Confidence:  0.85,
			RawLine:     line.Body,
			TTLSeconds:  1200,
		}, nil
	}

	return nil, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
