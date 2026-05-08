package daemon

import "fmt"

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func mapValue(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if result, ok := value.(map[string]any); ok {
		return result
	}
	if result, ok := value.(map[any]any); ok {
		converted := make(map[string]any, len(result))
		for k, v := range result {
			converted[fmt.Sprint(k)] = v
		}
		return converted
	}
	return map[string]any{}
}

func sliceMapValue(value any) []map[string]any {
	switch v := value.(type) {
	case []map[string]any:
		return v
	case []any:
		items := make([]map[string]any, 0, len(v))
		for _, item := range v {
			items = append(items, mapValue(item))
		}
		return items
	default:
		return []map[string]any{}
	}
}

func variablesValue(value any) map[string][]map[string]any {
	if typed, ok := value.(map[string][]map[string]any); ok {
		return typed
	}
	result := map[string][]map[string]any{}
	for key, raw := range mapValue(value) {
		result[key] = sliceMapValue(raw)
	}
	return result
}
