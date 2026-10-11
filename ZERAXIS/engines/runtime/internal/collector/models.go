package collector

// RawFalcoEvent represents a single JSON event emitted by Falco.
// This is the raw telemetry model — it must NOT be mapped directly
// into the Phase 4 unified envelope.
type RawFalcoEvent struct {
	Time         string                 `json:"time"`
	Rule         string                 `json:"rule"`
	Priority     string                 `json:"priority"`
	Source       string                 `json:"source"`
	Output       string                 `json:"output"`
	OutputFields map[string]interface{} `json:"output_fields"`
	Hostname     string                 `json:"hostname"`
	Tags         []string               `json:"tags"`
}

// GetStringField safely extracts a string field from OutputFields.
func (e *RawFalcoEvent) GetStringField(key string) string {
	if e.OutputFields == nil {
		return ""
	}
	v, ok := e.OutputFields[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// GetIntField safely extracts a numeric field from OutputFields as int.
func (e *RawFalcoEvent) GetIntField(key string) int {
	if e.OutputFields == nil {
		return 0
	}
	v, ok := e.OutputFields[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}
