package coreclient

import (
	"context"
	"net/http"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func (client *Client) LoadConfig(ctx context.Context) (generated.MutableConfigSnapshot, error) {
	response, err := client.api.GetConfigWithResponse(ctx)
	if err != nil {
		return generated.MutableConfigSnapshot{}, err
	}
	status := 0
	if response != nil {
		status = response.StatusCode()
	}
	if response == nil || status != http.StatusOK || response.JSON200 == nil {
		return generated.MutableConfigSnapshot{}, responseError("getConfig", status, problemCodeString(
			valueOrNil(response, func(value *generated.GetConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSONDefault
			}),
		))
	}
	return *response.JSON200, nil
}

func (client *Client) ValidateConfig(ctx context.Context, cmd generated.ValidateConfigCommand) (generated.ValidationResult, error) {
	response, err := client.api.ValidateConfigWithResponse(ctx, cmd)
	if err != nil {
		return generated.ValidationResult{}, err
	}
	status := 0
	if response != nil {
		status = response.StatusCode()
	}
	if response == nil || status != http.StatusOK || response.JSON200 == nil {
		return generated.ValidationResult{}, configMutationError("validateConfig", status, configProblem(
			valueOrNil(response, func(value *generated.ValidateConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON404
			}),
			valueOrNil(response, func(value *generated.ValidateConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON409
			}),
			valueOrNil(response, func(value *generated.ValidateConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON410
			}),
			valueOrNil(response, func(value *generated.ValidateConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON422
			}),
			valueOrNil(response, func(value *generated.ValidateConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSONDefault
			}),
		))
	}
	return *response.JSON200, nil
}

func (client *Client) PatchConfig(ctx context.Context, cmd generated.PatchConfigCommand) (generated.SnapshotIdentity, error) {
	response, err := client.api.PatchConfigWithResponse(ctx, cmd)
	if err != nil {
		return generated.SnapshotIdentity{}, err
	}
	status := 0
	if response != nil {
		status = response.StatusCode()
	}
	if response == nil || status != http.StatusOK || response.JSON200 == nil {
		return generated.SnapshotIdentity{}, configMutationError("patchConfig", status, configProblem(
			valueOrNil(response, func(value *generated.PatchConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON404
			}),
			valueOrNil(response, func(value *generated.PatchConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON409
			}),
			valueOrNil(response, func(value *generated.PatchConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON410
			}),
			valueOrNil(response, func(value *generated.PatchConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON422
			}),
			valueOrNil(response, func(value *generated.PatchConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON500
			}),
			valueOrNil(response, func(value *generated.PatchConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSON503
			}),
			valueOrNil(response, func(value *generated.PatchConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSONDefault
			}),
		))
	}
	return *response.JSON200, nil
}

func configProblem(problems ...*generated.Problem) *generated.Problem {
	for _, problem := range problems {
		if problem != nil {
			return problem
		}
	}
	return nil
}

func configMutationError(operation string, status int, problem *generated.Problem) error {
	code := ""
	detail := ""
	if problem != nil {
		code = problem.Code
		detail = configProblemDetail(problem)
	}
	if status == http.StatusOK || status == 0 {
		return responseError(operation, status, code)
	}
	return &APIError{Operation: operation, StatusCode: status, Code: code, Detail: detail}
}

func configProblemDetail(problem *generated.Problem) string {
	if problem == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if problem.Violations != nil {
		for _, violation := range *problem.Violations {
			path := strings.TrimSpace(violation.Path)
			message := strings.TrimSpace(violation.Message)
			switch {
			case path != "" && message != "":
				parts = append(parts, path+": "+message)
			case path != "":
				parts = append(parts, path)
			case message != "":
				parts = append(parts, message)
			}
		}
	}
	if len(parts) == 0 && problem.Detail != nil {
		if detail := strings.TrimSpace(*problem.Detail); detail != "" {
			parts = append(parts, detail)
		}
	}
	return strings.Join(parts, "; ")
}
