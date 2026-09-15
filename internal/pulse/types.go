package pulse

import (
	"time"
)

type Batch struct {
	Metrics []Metric `json:"metrics"`
	Events  []Event  `json:"events"`
	States  []State  `json:"states"`
}

type ResourceRef struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

func (r ResourceRef) Key() string {
	if r.Type == "" || r.ID == "" {
		return ""
	}
	return r.Type + ":" + r.ID
}

type Metric struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Value      float64           `json:"value"`
	Unit       string            `json:"unit,omitempty"`
	Timestamp  time.Time         `json:"ts"`
	Resource   *ResourceRef      `json:"resource,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

type Event struct {
	Kind          string            `json:"kind"`
	Timestamp     time.Time         `json:"ts"`
	Value         *float64          `json:"value,omitempty"`
	Resource      *ResourceRef      `json:"resource,omitempty"`
	Dimensions    map[string]string `json:"dimensions,omitempty"`
	Attributes    map[string]any    `json:"attributes,omitempty"`
	Sensitive     map[string]any    `json:"sensitive,omitempty"`
	ActorID       string            `json:"actorId,omitempty"`
	SessionID     string            `json:"sessionId,omitempty"`
	CorrelationID string            `json:"correlationId,omitempty"`
	Payload       map[string]any    `json:"payload,omitempty"`
}

type State struct {
	Key        string            `json:"key"`
	Value      any               `json:"value"`
	Timestamp  time.Time         `json:"ts"`
	Resource   *ResourceRef      `json:"resource,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}
