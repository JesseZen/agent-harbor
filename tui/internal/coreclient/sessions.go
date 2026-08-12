package coreclient

import (
	"context"
	"net/http"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

// LoadAgentSessions returns the generated authoritative Session views used by
// the K9s Sessions page. It deliberately does not project lifecycle or native
// activity into the legacy backend DTO.
func (client *Client) LoadAgentSessions(ctx context.Context) ([]generated.AgentSession, error) {
	response, err := client.api.ListAgentSessionsWithResponse(ctx, &generated.ListAgentSessionsParams{
		InstanceId: generated.InstanceID(client.expectedInstanceID),
	})
	if err != nil {
		return nil, err
	}
	status := 0
	if response != nil {
		status = response.StatusCode()
	}
	if response == nil || status != http.StatusOK || response.JSON200 == nil {
		return nil, responseError("listAgentSessions", status, problemCode(
			valueOrNil(response, func(value *generated.ListAgentSessionsResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON500
			}),
			valueOrNil(response, func(value *generated.ListAgentSessionsResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSONDefault
			}),
		))
	}
	return append([]generated.AgentSession(nil), (*response.JSON200)...), nil
}
