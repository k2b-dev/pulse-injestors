package pulse

import (
	"encoding/json"
	"fmt"
)

// SplitBatch splits a batch into parts within Pulse's ingest limits.
func SplitBatch(batch Batch) ([]Batch, error) {
	return splitBatch(batch, maxItemsPerIngestCollection, maxItemsPerIngestPayload, maxBytesPerIngestPayload)
}

func splitBatch(batch Batch, collectionLimit, itemLimit, byteLimit int) ([]Batch, error) {
	if collectionLimit <= 0 {
		collectionLimit = maxItemsPerIngestCollection
	}
	if itemLimit <= 0 {
		itemLimit = maxItemsPerIngestPayload
	}
	if byteLimit <= 0 {
		byteLimit = maxBytesPerIngestPayload
	}
	// Each item is encoded once; the part size is tracked incrementally.
	overhead := encodedSize(emptyBatch())
	parts := []Batch{}
	current, size := emptyBatch(), overhead
	add := func(kind string, value any, count int) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode pulse %s: %w", kind, err)
		}
		itemSize := len(encoded)
		if count > 0 {
			itemSize++ // separating comma
		}
		if overhead+len(encoded) > byteLimit {
			return fmt.Errorf("single pulse %s exceeds ingest payload byte limit", kind)
		}
		if batchItems(current)+1 > itemLimit || count+1 > collectionLimit || size+itemSize > byteLimit {
			parts = append(parts, current)
			current, size = emptyBatch(), overhead
			itemSize = len(encoded)
		}
		switch kind {
		case "metric":
			current.Metrics = append(current.Metrics, value.(Metric))
		case "event":
			current.Events = append(current.Events, value.(Event))
		case "state":
			current.States = append(current.States, value.(State))
		}
		size += itemSize
		return nil
	}
	for _, metric := range batch.Metrics {
		if err := add("metric", metric, len(current.Metrics)); err != nil {
			return nil, err
		}
	}
	for _, event := range batch.Events {
		if err := add("event", event, len(current.Events)); err != nil {
			return nil, err
		}
	}
	for _, state := range batch.States {
		if err := add("state", state, len(current.States)); err != nil {
			return nil, err
		}
	}
	if batchItems(current) > 0 {
		parts = append(parts, current)
	}
	return parts, nil
}

func emptyBatch() Batch {
	return Batch{Metrics: []Metric{}, Events: []Event{}, States: []State{}}
}

func batchItems(batch Batch) int {
	return len(batch.Metrics) + len(batch.Events) + len(batch.States)
}

func encodedSize(batch Batch) int {
	body, err := json.Marshal(batch)
	if err != nil {
		return maxBytesPerIngestPayload + 1
	}
	return len(body)
}
