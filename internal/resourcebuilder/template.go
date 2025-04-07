package resourcebuilder

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	"github.com/unbindapp/unbind-api/pkg/templates"
	"k8s.io/apimachinery/pkg/runtime"
)

func (rb *ResourceBuilder) BuildTemplate(ctx context.Context, logger logr.Logger) ([]runtime.Object, error) {
	// * Template path
	templateProvider := templates.NewUnbindTemplateProvider()
	templateRenderer := templates.NewTemplateRenderer()
	fetchedTemplate, err := templateProvider.FetchTemplate(ctx,
		rb.service.Spec.Config.Template.VersionRef,
		rb.service.Spec.Config.Template.Category,
		rb.service.Spec.Config.Template.Name,
	)

	if err != nil || fetchedTemplate == nil {
		logger.Error(err, "Failed to fetch template")
		return nil, err
	}

	// Convert the template config to a map
	templateConfig, err := rawExtensionToMap(rb.service.Spec.Config.Template.Config)
	if err != nil {
		logger.Error(err, "Failed to convert template config to map")
	}

	// Add labels to the template config
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
	renderedYaml, err := templateRenderer.Render(fetchedTemplate, &templates.RenderContext{
		Name:          rb.service.Name,
		Namespace:     rb.service.Namespace,
		TeamID:        rb.service.Spec.TeamRef,
		ProjectID:     rb.service.Spec.ProjectRef,
		EnvironmentID: rb.service.Spec.EnvironmentRef,
		ServiceID:     rb.service.Spec.ServiceRef,
		Template:      *fetchedTemplate,
		Parameters:    templateConfig,
	})

	if err != nil {
		logger.Error(err, "Failed to render template")
		return nil, err
	}

	// Create as resource
	return templateRenderer.RenderToObjects(renderedYaml)
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
