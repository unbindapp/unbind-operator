package resourcebuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/unbindapp/unbind-api/pkg/databases"
	"k8s.io/apimachinery/pkg/runtime"
)

// BuildDatabaseObjects renders and returns Kubernetes objects for the database
func (rb *ResourceBuilder) BuildDatabaseObjects(ctx context.Context, logger logr.Logger) ([]runtime.Object, error) {
	// * Database path
	dbProvider := databases.NewDatabaseProvider()
	dbRenderer := databases.NewDatabaseRenderer()

	fetchedDb, err := dbProvider.FetchDatabaseDefinition(ctx,
		rb.service.Spec.Config.Database.DatabaseSpecVersion,
		rb.service.Spec.Config.Database.Type,
	)
	if err != nil || fetchedDb == nil {
		logger.Error(err, "Failed to fetch database")
		return nil, err
	}

	// Convert the database config to a map
	dbConfig, err := rawExtensionToMap(rb.service.Spec.Config.Database.Config)
	if err != nil {
		logger.Error(err, "Failed to convert database config to map")
		return nil, err
	}

	// Add labels to the database config
	_, ok := dbConfig["labels"]
	if !ok {
		dbConfig["labels"] = make(map[string]string)
	}

	labelsMap, ok := dbConfig["labels"].(map[string]interface{})
	if !ok {
		// Try to convert to the expected format
		if strMap, ok := dbConfig["labels"].(map[string]string); ok {
			// Convert map[string]string to map[string]interface{}
			labelsMap = make(map[string]interface{})
			for k, v := range strMap {
				labelsMap[k] = v
			}
			dbConfig["labels"] = labelsMap
		} else {
			// Create a new map if conversion fails
			dbConfig["labels"] = make(map[string]interface{})
			labelsMap = dbConfig["labels"].(map[string]interface{})
		}
	}

	// Append common labels
	for k, v := range rb.getCommonLabels() {
		labelsMap[k] = v
	}

	// Append env
	_, ok = dbConfig["environment"]
	if !ok {
		dbConfig["environment"] = make(map[string]string)
	}

	envMap, ok := dbConfig["environment"].(map[string]interface{})
	if !ok {
		// Try to convert to the expected format
		if strMap, ok := dbConfig["environment"].(map[string]string); ok {
			// Convert map[string]string to map[string]interface{}
			envMap = make(map[string]interface{})
			for k, v := range strMap {
				envMap[k] = v
			}
			dbConfig["environment"] = envMap
		} else {
			// Create a new map if conversion fails
			dbConfig["environment"] = make(map[string]interface{})
			envMap = dbConfig["environment"].(map[string]interface{})
		}
	}

	// Add environment variables from service
	for _, v := range rb.service.Spec.EnvVars {
		envMap[v.Name] = v.Value
	}

	// Get common config, if it exists as a key
	_, ok = dbConfig["common"]
	if !ok {
		dbConfig["common"] = make(map[string]interface{})
	}

	commonMap, ok := dbConfig["common"].(map[string]interface{})
	if !ok {
		// Create a new map if conversion fails
		dbConfig["common"] = make(map[string]interface{})
		commonMap = dbConfig["common"].(map[string]interface{})
	}

	// Set replica count from service config
	var replicaCount int32 = 1 // Default replica count if pointer is nil or not specified
	if rb.service.Spec.Config.Replicas != nil {
		replicaCount = *rb.service.Spec.Config.Replicas
	}
	commonMap["replicas"] = replicaCount

	// For database-specific settings
	if rb.service.Spec.Config.Public {
		if rb.service.Spec.Config.Database.Type == "postgres" {
			dbConfig["enableMasterLoadBalancer"] = true
		}
	}

	// Add kubernetes secret name if specified in the service
	if rb.service.Spec.KubernetesSecret != "" {
		// For redis
		if strings.EqualFold(fetchedDb.Name, "redis") {
			dbConfig["secretName"] = rb.service.Spec.KubernetesSecret
			dbConfig["secretKey"] = "REDIS_PASSWORD"
		}
	}

	// Render the database definition
	renderedYaml, err := dbRenderer.Render(fetchedDb, &databases.RenderContext{
		Name:       rb.service.Name,
		Namespace:  rb.service.Namespace,
		TeamID:     rb.service.Spec.TeamRef,
		Definition: *fetchedDb,
		Parameters: dbConfig,
	})
	if err != nil {
		logger.Error(err, "Failed to render database definition",
			"type", fetchedDb.Type,
			"name", fetchedDb.Name)
		return nil, err
	}

	// Create resources from the rendered YAML
	objects, err := dbRenderer.RenderToObjects(renderedYaml)
	if err != nil {
		logger.Error(err, "Failed to create runtime objects from rendered YAML",
			"type", fetchedDb.Type,
			"name", fetchedDb.Name)
		return nil, err
	}

	// Process the created objects if needed (e.g., add additional labels or annotations)
	processedObjects, err := rb.processRenderedObjects(objects, logger)
	if err != nil {
		logger.Error(err, "Failed to process rendered objects")
		return nil, err
	}

	return processedObjects, nil
}

// processRenderedObjects allows for custom processing of rendered database objects
func (rb *ResourceBuilder) processRenderedObjects(objects []runtime.Object, logger logr.Logger) ([]runtime.Object, error) {
	result := make([]runtime.Object, 0, len(objects))

	for _, obj := range objects {
		// Log the type of object being processed
		logger.Info("Processing rendered object", "kind", fmt.Sprintf("%T", obj))

		// You can add custom processing for specific object types here
		// For example, add owner references, additional labels, etc.

		// Add the processed object to the result
		result = append(result, obj)
	}

	return result, nil
}

// Helper function to convert runtime.RawExtension to map[string]interface{}
func rawExtensionToMap(raw runtime.RawExtension) (map[string]interface{}, error) {
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
