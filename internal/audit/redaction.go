// Package audit 包提供审计事件写入、查询和敏感字段脱敏能力。
package audit

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

const redactedValue = "****"

var windowsUserPathPattern = regexp.MustCompile(`(?i)([A-Z]:/Users/)[^/\\]+`)

// RedactValue 对单个值做审计脱敏。
func RedactValue(key string, value any) any {
	if isSensitiveKey(key) {
		return redactedValue
	}
	switch v := value.(type) {
	case string:
		return redactString(v)
	case map[string]any:
		return redactMap(v)
	case []any:
		for i := range v {
			v[i] = RedactValue("", v[i])
		}
		return v
	default:
		return value
	}
}

// RedactJSON 对 JSON 对象做递归脱敏。
func RedactJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	value = RedactValue("", value)
	return json.Marshal(value)
}

func redactMap(values map[string]any) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = RedactValue(key, value)
	}
	return result
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, marker := range []string{"password", "secret", "token", "credential", "jwt"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

func redactString(value string) string {
	if value == "" {
		return value
	}
	if u, err := url.Parse(value); err == nil && u.User != nil {
		u.User = url.User(redactedValue)
		value = u.String()
	}
	return windowsUserPathPattern.ReplaceAllString(value, `${1}`+redactedValue)
}
