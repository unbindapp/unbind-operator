package resourcebuilder

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	"github.com/unbindapp/unbind-api/pkg/databases"
	"k8s.io/apimachinery/pkg/runtime"
)

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
	}

	// Add labels to the database config
	_, ok := dbConfig["labels"]
	if !ok {
		dbConfig["labels"] = make(map[string]string)
	}
	_, ok = dbConfig["labels"].(map[string]string)
	if !ok {
		logger.Error(err, "Failed to convert labels to map")
		return nil, err
	}

	// Append the maps
	for k, v := range rb.getCommonLabels() {
		dbConfig["labels"].(map[string]string)[k] = v
	}

	// Append env
	dbConfig["environment"] = make(map[string]string)
	for _, v := range rb.service.Spec.EnvVars {
		dbConfig["environment"].(map[string]string)[v.Name] = v.Value
	}

	// Get common config, if it exists as a key
	_, ok = dbConfig["common"]
	if !ok {
		dbConfig["common"] = make(map[string]interface{})
	}

	dbConfig["common"].(map[string]interface{})["replicas"] = rb.service.Spec.Config.Replicas

	if rb.service.Spec.Config.Public {
		if rb.service.Spec.Config.Database.Type == "postgres" {
			dbConfig["enableMasterLoadBalancer"] = true
		}
	}

	// Render the db
	renderedYaml, err := dbRenderer.Render(fetchedDb, &databases.RenderContext{
		Name:       rb.service.Name,
		Namespace:  rb.service.Namespace,
		TeamID:     rb.service.Spec.TeamRef,
		Definition: *fetchedDb,
		Parameters: dbConfig,
	})

	if err != nil {
		logger.Error(err, "Failed to render database db")
		return nil, err
	}

	// Create as resource
	return dbRenderer.RenderToObjects(renderedYaml)
}

// Helper function to convert runtime.RawExtension to map[string]interface{}
func rawExtensionToMap(raw runtime.RawExtension) (map[string]interface{}, error) {
	if raw.Raw == nil {
		return nil, nil
	}

	var result map[string]interface{}
	err := json.Unmarshal(raw.Raw, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}
