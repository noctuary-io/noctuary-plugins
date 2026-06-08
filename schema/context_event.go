package schema

// EventType classifies the kind of state change a ContextEvent represents.
type EventType string

const (
	EventTypeDeploy            EventType = "deploy"
	EventTypeFlagChange        EventType = "flag_change"
	EventTypeConfigChange      EventType = "config_change"
	EventTypeRestart           EventType = "restart"
	EventTypeCircuitOpen       EventType = "circuit_open"
	EventTypeSaturation        EventType = "saturation"
	EventTypeDependencyFailure EventType = "dependency_failure"
	EventTypeSchemaChange      EventType = "schema_change"
	EventTypeScaleEvent        EventType = "scale_event"
)

// ContextEvent is the normalised output shape every vendor plugin emits.
// All fields use the same names regardless of which vendor produced the event.
type ContextEvent struct {
	EventType   EventType `json:"event_type"`
	Vendor      string    `json:"vendor"`
	ServiceName string    `json:"service_name"`
	Entity      string    `json:"entity"`
	OldValue    string    `json:"old_value,omitempty"`
	NewValue    string    `json:"new_value,omitempty"`
	Actor       string    `json:"actor,omitempty"`
	SHA         string    `json:"sha,omitempty"`
	Timestamp   string    `json:"timestamp"`
	Confidence  float64   `json:"confidence"`
	RawLine     string    `json:"raw_line"`
	TTLSeconds  int       `json:"ttl_seconds"`
}
