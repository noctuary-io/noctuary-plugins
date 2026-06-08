package schema

// ParsedOTelLog is what the OTel structural parser hands downstream.
// It captures the standard OTel fields; Body holds the raw log line
// exactly as the vendor tool emitted it.
type ParsedOTelLog struct {
	// CustomerID identifies which customer's agent produced this log stream.
	// Set by the agent at startup from its configuration — not derived from the log.
	CustomerID  string            `json:"customer_id,omitempty"`
	ServiceName string            `json:"service_name"`
	Namespace   string            `json:"namespace,omitempty"`
	Severity    string            `json:"severity"`
	Timestamp   string            `json:"timestamp"`
	Body        string            `json:"body"`
	Attributes  map[string]string `json:"attributes,omitempty"`
}
