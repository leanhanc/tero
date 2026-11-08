// Package variables is how the agent gets an app's values from the key service
// for a deploy.
package variables

import (
	"context"
	"strings"

	"github.com/leanhanc/tero/internal/containers"
	"github.com/leanhanc/tero/internal/keys"
)

// minMaskedBytes keeps short unsealed values such as "true" or "8080" out of
// log masking, where they would ruin the log. Sealed values are always masked.
const minMaskedBytes = 8

// Releaser is the key service's verb for the agent.
type Releaser interface {
	ReleaseVariables(ctx context.Context, release keys.ReleaseRequest) ([]keys.ReleasedVariable, error)
}

// Released is an app's values for one phase of a deploy.
type Released []keys.ReleasedVariable

// Map is the values by name.
func (r Released) Map() map[string]string {
	values := make(map[string]string, len(r))
	for _, variable := range r {
		values[variable.Name] = variable.Value
	}

	return values
}

// Masked is the values to hide in logs. A multi-line value also adds each of
// its lines long enough to mask, since build output puts a prefix on every line
// and the whole value never appears as written.
func (r Released) Masked() []string {
	masked := []string{}
	for _, variable := range r {
		if variable.IsSealed || len(variable.Value) >= minMaskedBytes {
			masked = append(masked, variable.Value)
		}
		if !strings.Contains(variable.Value, "\n") {
			continue
		}
		for _, line := range strings.Split(variable.Value, "\n") {
			line = strings.TrimSpace(line)
			if len(line) >= minMaskedBytes {
				masked = append(masked, line)
			}
		}
	}

	return masked
}

// ForBuild asks for the values a build of source gets.
func ForBuild(ctx context.Context, releaser Releaser, app string, source keys.AppSource, deployID int64) (Released, error) {
	return releaser.ReleaseVariables(ctx, keys.ReleaseRequest{App: app, Source: source, Phase: keys.PhaseBuild, DeployID: deployID})
}

// ForImage asks for the values a container of image gets, for the source
// recorded in the image when it was built rather than the app's row in the
// database, which the dashboard can write. Otherwise a compromised dashboard
// could build its own code under this app with the row pointed elsewhere,
// restore the row and restart that image.
func ForImage(ctx context.Context, releaser Releaser, app, image string, deployID int64) (Released, error) {
	source, err := containers.ImageSource(ctx, app, image)
	if err != nil {
		return nil, err
	}

	return releaser.ReleaseVariables(ctx, keys.ReleaseRequest{App: app, Source: source, Phase: keys.PhaseRun, DeployID: deployID})
}
