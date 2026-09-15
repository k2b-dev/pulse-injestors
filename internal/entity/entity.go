package entity

import (
	"net/url"
	"strings"
)

func StableHostID(value string) string { return strings.TrimSpace(value) }

func StableHostIDFromScope(resourceID string, dimensions map[string]string) string {
	if dimensions != nil {
		if host := StableHostID(dimensions["host"]); host != "" {
			return host
		}
	}
	return StableHostID(resourceID)
}

func ID(parts ...string) string {
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = component(part)
	}
	return strings.Join(out, ":")
}

func Key(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func component(value string) string { return strings.ReplaceAll(url.PathEscape(value), ":", "%3A") }
