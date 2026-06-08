// Package generic is the catch-all vendor plugin for any OTel-instrumented service.
// It operates purely on OTel envelope fields — severity and exception semantic
// conventions — and never parses the log body. It deliberately scores at exactly
// the confidence threshold so specific plugins always win when they fire.
package generic

import (
	"strings"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "generic"
	version    = "0.1.0"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	// Empty substring matches every line — gives this plugin a score of exactly
	// 0.5 (the default threshold) so specific plugins win whenever they score higher.
	return []plugin.FingerprintRule{
		{Type: plugin.RuleTypeSubstring, Value: "", Weight: 0.5},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	sev := strings.ToUpper(line.Severity)
	isError := strings.HasPrefix(sev, "ERROR")
	isFatal := strings.HasPrefix(sev, "FATAL")
	hasException := line.Attributes["exception.type"] != ""

	if !isError && !isFatal && !hasException {
		return nil, nil
	}

	entity := line.ServiceName
	if entity == "" {
		entity = "unknown"
	}

	eventType := schema.EventTypeDependencyFailure
	if isFatal {
		eventType = schema.EventTypeRestart
	}

	newValue := ""
	if excType := line.Attributes["exception.type"]; excType != "" {
		newValue = excType
		if excMsg := line.Attributes["exception.message"]; excMsg != "" {
			newValue += ": " + excMsg
		}
	} else {
		n := min(80, len(line.Body))
		newValue = line.Body[:n]
	}

	return &schema.ContextEvent{
		EventType:   eventType,
		Vendor:      vendorName,
		ServiceName: line.ServiceName,
		Entity:      entity,
		NewValue:    newValue,
		Timestamp:   line.Timestamp,
		Confidence:  0.4,
		RawLine:     line.Body,
		TTLSeconds:  900,
	}, nil
}
