package configdraft

import (
	"encoding/json"
	"sort"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func ViewToCommand(view generated.MutableConfigView) generated.MutableConfigCommand {
	cmd := generated.MutableConfigCommand{
		Instance:                view.Instance,
		ManagedObjects:          cloneManagedObjects(view.ManagedObjects),
		BackendSets:             append([]generated.BackendSetConfig{}, view.BackendSets...),
		ClientProfiles:          append([]generated.MutableClientProfile{}, view.ClientProfiles...),
		CompatibilityTransforms: append([]generated.CompatibilityTransformConfig{}, view.CompatibilityTransforms...),
		ContentPolicies:         append([]generated.ContentPolicyConfig{}, view.ContentPolicies...),
		Endpoints:               append([]generated.EndpointConfig{}, view.Endpoints...),
		ModelPolicies:           append([]generated.ModelPolicyConfig{}, view.ModelPolicies...),
		ModelProjections:        append([]generated.ModelProjectionConfig{}, view.ModelProjections...),
		QuotaGroups:             append([]generated.QuotaGroupConfig{}, view.QuotaGroups...),
		Routes:                  append([]generated.RouteConfig{}, view.Routes...),
		Credentials:             make([]generated.MutableCredentialCommand, 0, len(view.Credentials)),
		Targets:                 make([]generated.MutableTargetCommand, 0, len(view.Targets)),
	}
	for _, cred := range view.Credentials {
		cmd.Credentials = append(cmd.Credentials, credentialViewToCommand(cred))
	}
	for _, target := range view.Targets {
		cmd.Targets = append(cmd.Targets, TargetToCommand(target))
	}
	return cmd
}

func cloneManagedObjects(objects *[]generated.ManagedObject) *[]generated.ManagedObject {
	if objects == nil {
		return nil
	}
	cloned := append([]generated.ManagedObject(nil), (*objects)...)
	for i := range cloned {
		cloned[i].Members = append([]generated.ManagedResourceRef(nil), cloned[i].Members...)
	}
	return &cloned
}

func TargetToCommand(t generated.TargetConfig) generated.MutableTargetCommand {
	return generated.MutableTargetCommand{
		Id:             t.Id,
		Name:           t.Name,
		Adapter:        generated.MutableTargetCommandAdapter(t.Adapter),
		Bridge:         generated.MutableTargetCommandBridge(t.Bridge),
		Capabilities:   capabilitiesToCommand(t.Capabilities),
		CredentialId:   t.CredentialId,
		EndpointId:     t.EndpointId,
		QuotaGroupId:   t.QuotaGroupId,
		HealthPolicy:   t.HealthPolicy,
		ThrottlePolicy: t.ThrottlePolicy,
	}
}

func credentialViewToCommand(v generated.MutableCredentialView) generated.MutableCredentialCommand {
	cmd := generated.MutableCredentialCommand{
		Id:       v.Id,
		Name:     v.Name,
		Provider: generated.MutableCredentialCommandProvider(v.Provider),
	}
	if managed, err := v.SecretBinding.AsCredentialSecretBinding0(); err == nil && managed.Mode == generated.CredentialSecretBindingManaged {
		var action generated.CredentialSecretAction
		_ = action.FromCredentialSecretAction0(generated.CredentialSecretAction0{
			Mode: generated.CredentialSecretActionPreserve,
		})
		cmd.SecretAction = action
		return cmd
	}
	if external, err := v.SecretBinding.AsCredentialSecretBinding1(); err == nil && external.Mode == generated.CredentialSecretBindingExternalRef {
		var action generated.CredentialSecretAction
		_ = action.FromCredentialSecretAction2(generated.CredentialSecretAction2{
			Mode: generated.CredentialSecretActionExternalRef,
			Ref:  external.Ref,
		})
		cmd.SecretAction = action
		return cmd
	}
	var action generated.CredentialSecretAction
	_ = action.FromCredentialSecretAction0(generated.CredentialSecretAction0{
		Mode: generated.CredentialSecretActionPreserve,
	})
	cmd.SecretAction = action
	return cmd
}

func capabilitiesToCommand(caps []generated.TargetConfigCapabilities) []generated.MutableTargetCommandCapabilities {
	out := make([]generated.MutableTargetCommandCapabilities, len(caps))
	for i, c := range caps {
		out[i] = generated.MutableTargetCommandCapabilities(c)
	}
	return out
}

func jsonEqual(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

func canonicalSetJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err != nil {
		bb, _ := json.Marshal(v)
		return string(bb)
	}
	sort.Slice(arr, func(i, j int) bool {
		return string(arr[i]) < string(arr[j])
	})
	out, _ := json.Marshal(arr)
	return string(out)
}

func setEqual(a, b any) bool {
	return canonicalSetJSON(a) == canonicalSetJSON(b)
}

func credentialWithoutGeneration(c generated.MutableCredentialCommand) generated.MutableCredentialCommand {
	return c
}

func cloneCommand(cmd generated.MutableConfigCommand) generated.MutableConfigCommand {
	b, _ := json.Marshal(cmd)
	var out generated.MutableConfigCommand
	_ = json.Unmarshal(b, &out)
	return out
}

func canonicalCommand(cmd generated.MutableConfigCommand) generated.MutableConfigCommand {
	return cloneCommand(cmd)
}

func cloneView(view generated.MutableConfigView) generated.MutableConfigView {
	b, _ := json.Marshal(view)
	var out generated.MutableConfigView
	_ = json.Unmarshal(b, &out)
	return out
}
