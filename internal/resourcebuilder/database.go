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
	templateConfig, err := rawExtensionToMap(rb.service.Spec.Config.Database.Config)
	if err != nil {
		logger.Error(err, "Failed to convert database config to map")
	}

	// Add labels to the database config
	_, ok := templateConfig["labels"]
	if !ok {
		templateConfig["labels"] = make(map[string]string)
	}
	_, ok = templateConfig["labels"].(map[string]string)
	if !ok {
		logger.Error(err, "Failed to convert labels to map")
		return nil, err
	}

	// Append the maps
	for k, v := range rb.getCommonLabels() {
		templateConfig["labels"].(map[string]string)[k] = v
	}

	// Render the template
	renderedYaml, err := dbRenderer.Render(fetchedDb, &databases.RenderContext{
		Name:          rb.service.Name,
		Namespace:     rb.service.Namespace,
		TeamID:        rb.service.Spec.TeamRef,
		ProjectID:     rb.service.Spec.ProjectRef,
		EnvironmentID: rb.service.Spec.EnvironmentRef,
		ServiceID:     rb.service.Spec.ServiceRef,
		Definition:    *fetchedDb,
		Parameters:    templateConfig,
	})

	if err != nil {
		logger.Error(err, "Failed to render database template")
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
