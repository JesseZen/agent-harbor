package coreclient

import (
	"context"
	"net/http"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

// ProbeTarget queues a target connectivity check. Runtime invalidations carry
// the eventual health result, so this call only waits for Core to accept it.
func (client *Client) ProbeTarget(ctx context.Context, targetID string, generation int, timeout time.Duration) error {
	response, err := client.api.ProbeTargetWithResponse(ctx, generated.TargetID(targetID), generated.TargetProbeCommand{
		InstanceId:               client.expectedInstanceID,
		ExpectedTargetGeneration: generation,
		TimeoutMs:                timeout.Milliseconds(),
	})
	if err != nil {
		return err
	}
	if response == nil || response.StatusCode() != http.StatusAccepted || response.JSON202 == nil {
		return actionResponseError("probeTarget", responseStatus(response))
	}
	return nil
}

func (client *Client) DiscoverTargetModels(ctx context.Context, targetID string) ([]string, error) {
	response, err := client.api.DiscoverTargetModelsWithResponse(ctx, generated.TargetID(targetID))
	if err != nil {
		return nil, err
	}
	status := 0
	if response != nil {
		status = response.StatusCode()
	}
	if response == nil || status != http.StatusOK || response.JSON200 == nil {
		return nil, phase04ResponseError("discoverTargetModels", status,
			valueOrNil(response, func(value *generated.DiscoverTargetModelsResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON404
			}),
			valueOrNil(response, func(value *generated.DiscoverTargetModelsResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON500
			}),
			valueOrNil(response, func(value *generated.DiscoverTargetModelsResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSONDefault
			}),
		)
	}
	models := make([]string, 0, len(response.JSON200.Models))
	for _, model := range response.JSON200.Models {
		models = append(models, model.Id)
	}
	return models, nil
}
