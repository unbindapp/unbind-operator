package utils

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime"
)

// Helper function to convert runtime.RawExtension to map[string]interface{}
func RawExtensionToMap(raw runtime.RawExtension) (map[string]interface{}, error) {
	if raw.Raw == nil {
		return make(map[string]interface{}), nil
	}

	var result map[string]interface{}
	err := json.Unmarshal(raw.Raw, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}
