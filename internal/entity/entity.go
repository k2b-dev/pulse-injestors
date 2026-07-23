package entity

import "strings"

func StableHostID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "host:")
	value = strings.TrimPrefix(value, "/")
	if value == "" {
		return ""
	}
	return component(value)
}

func HostID(stableHostID string) string {
	return "host:" + component(stableHostID)
}

func StableHostIDFromScope(entityID string, dimensions map[string]string) string {
	if dimensions != nil {
		if host := StableHostID(dimensions["host"]); host != "" {
			return host
		}
	}
	return StableHostID(entityID)
}

func ID(kind string, parts ...string) string {
	out := kind
	for _, part := range parts {
		out += ":" + component(part)
	}
	return out
}

func Key(value, fallback string) string {
	key := component(value)
	if key == "" {
		return component(fallback)
	}
	return key
}

func component(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "/")
	replacer := strings.NewReplacer(":", "_", "/", "_", " ", "_", "\t", "_", "\n", "_")
	return replacer.Replace(value)
}
