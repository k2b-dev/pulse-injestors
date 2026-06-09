package pulse

import "time"

type Batch struct {
	Metrics []Metric `json:"metrics"`
	Events  []Event  `json:"events"`
	States  []State  `json:"states"`
}

type Metric struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Value      float64           `json:"value"`
	Unit       string            `json:"unit,omitempty"`
	Timestamp  time.Time         `json:"ts"`
	EntityID   string            `json:"entityId,omitempty"`
	EntityType string            `json:"entityType,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

type Event struct {
	Kind       string            `json:"kind"`
	Timestamp  time.Time         `json:"ts"`
	EntityID   string            `json:"entityId,omitempty"`
	EntityType string            `json:"entityType,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
	Payload    map[string]any    `json:"payload,omitempty"`
}

type State struct {
	Key        string            `json:"key"`
	Value      any               `json:"value"`
	Timestamp  time.Time         `json:"ts"`
	EntityID   string            `json:"entityId,omitempty"`
	EntityType string            `json:"entityType,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}
