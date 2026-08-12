package coreclient

import (
	"context"
	"fmt"
	"io"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

// CreateProviderSecretStageWithBodyWithResponse exposes the generated stage
// operation to the resource host without duplicating transport or response
// parsing.
func (client *Client) CreateProviderSecretStageWithBodyWithResponse(
	ctx context.Context,
	contentType string,
	body io.Reader,
	reqEditors ...generated.RequestEditorFn,
) (*generated.CreateProviderSecretStageResponse, error) {
	return client.api.CreateProviderSecretStageWithBodyWithResponse(ctx, contentType, body, reqEditors...)
}

// DeleteProviderSecretStageWithResponse exposes the generated stage cleanup
// operation to the resource host.
func (client *Client) DeleteProviderSecretStageWithResponse(
	ctx context.Context,
	stageID generated.SecretStageID,
	reqEditors ...generated.RequestEditorFn,
) (*generated.DeleteProviderSecretStageResponse, error) {
	return client.api.DeleteProviderSecretStageWithResponse(ctx, stageID, reqEditors...)
}

// LoadConfigMutationStatus returns the readiness-owned publication gate.
func (client *Client) LoadConfigMutationStatus(ctx context.Context) (generated.ConfigMutationStatus, error) {
	readiness, err := client.getReadiness(ctx)
	if err != nil {
		return "", err
	}
	if !readiness.ConfigMutationStatus.Valid() {
		return "", fmt.Errorf("%w: invalid config mutation status %q", ErrProtocolIncompatible, readiness.ConfigMutationStatus)
	}
	return readiness.ConfigMutationStatus, nil
}
