package utils

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/unbindapp/unbind-operator/internal/errors"
)

// InferPortFromImage pulls the image manifest and returns the first exposed port found.
func InferPortFromImage(image string, auth *authn.Basic) (int32, error) {
	// Pull the image from the registry.
	opts := []crane.Option{}
	if auth != nil {
		opts = append(opts, crane.WithAuth(auth))
	}
	img, err := crane.Pull(image, opts...)
	if err != nil {
		return 0, fmt.Errorf("failed to pull image: %w", err)
	}

	// Get the image configuration.
	configFile, err := img.ConfigFile()
	if err != nil {
		return 0, fmt.Errorf("failed to get image config: %w", err)
	}

	// Iterate over the ExposedPorts map.
	for exposed := range configFile.Config.ExposedPorts {
		// The key is typically in the form "80/tcp".
		parts := strings.Split(exposed, "/")
		if len(parts) > 0 {
			var port int
			// Parse the port number.
			_, err := fmt.Sscanf(parts[0], "%d", &port)
			if err == nil && port > 0 {
				return int32(port), nil
			}
		}
	}

	return 0, errors.NewCustomError(errors.ErrorInferringPortFromImage, fmt.Sprintf("no exposed port found in image %s", image))
}
