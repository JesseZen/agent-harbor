package configdraft

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func mergeThreeWay[T any](
	path string,
	base, current, draft T,
	merge func(path string, base, current, draft T) (T, []Conflict),
) (T, []Conflict) {
	if jsonEqual(draft, base) {
		return current, nil
	}
	if jsonEqual(current, base) {
		return draft, nil
	}
	if jsonEqual(draft, current) {
		return draft, nil
	}
	return merge(path, base, current, draft)
}

func mergeInstance(path string, base, current, draft generated.MutableInstanceConfig) (generated.MutableInstanceConfig, []Conflict) {
	return mergeThreeWay(path, base, current, draft, func(p string, b, c, d generated.MutableInstanceConfig) (generated.MutableInstanceConfig, []Conflict) {
		out := d
		var conflicts []Conflict
		if b.LogLevel != d.LogLevel && c.LogLevel != d.LogLevel && b.LogLevel != c.LogLevel {
			conflicts = append(conflicts, Conflict{Path: p + ".log_level", Reason: "field diverged"})
		} else if jsonEqual(d.LogLevel, b.LogLevel) {
			out.LogLevel = c.LogLevel
		} else if jsonEqual(c.LogLevel, b.LogLevel) {
			out.LogLevel = d.LogLevel
		} else {
			out.LogLevel = d.LogLevel
		}
		return out, conflicts
	})
}

func indexByID[T any](items []T, idFn func(T) generated.ConfigID) map[generated.ConfigID]T {
	out := make(map[generated.ConfigID]T, len(items))
	for _, item := range items {
		out[idFn(item)] = item
	}
	return out
}

func mergeIDCollection[T any](
	path string,
	base, current, draft []T,
	idFn func(T) generated.ConfigID,
	fieldMerge func(path string, base, current, draft T) (T, []Conflict),
) ([]T, []Conflict) {
	baseMap := indexByID(base, idFn)
	currentMap := indexByID(current, idFn)
	draftMap := indexByID(draft, idFn)

	ids := mapKeysUnion(baseMap, currentMap, draftMap)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var out []T
	var conflicts []Conflict

	for _, id := range ids {
		itemPath := fmt.Sprintf("%s[%s]", path, id)
		_, inBase := baseMap[id]
		_, inCurrent := currentMap[id]
		_, inDraft := draftMap[id]

		switch {
		case inBase && !inDraft && !inCurrent:
			continue
		case inBase && !inDraft && inCurrent:
			conflicts = append(conflicts, Conflict{Path: itemPath, Reason: "deleted locally but modified remotely"})
		case inBase && inDraft && !inCurrent:
			conflicts = append(conflicts, Conflict{Path: itemPath, Reason: "modified locally but deleted remotely"})
		case !inBase && inDraft && !inCurrent:
			out = append(out, draftMap[id])
		case !inBase && inDraft && inCurrent:
			if !jsonEqual(draftMap[id], currentMap[id]) {
				conflicts = append(conflicts, Conflict{Path: itemPath, Reason: "same id different new resources"})
			} else {
				out = append(out, draftMap[id])
			}
		case !inBase && !inDraft && inCurrent:
			out = append(out, currentMap[id])
		default:
			b, bOK := baseMap[id]
			c, cOK := currentMap[id]
			d, dOK := draftMap[id]
			if !bOK || !cOK || !dOK {
				continue
			}
			merged, itemConflicts := fieldMerge(itemPath, b, c, d)
			conflicts = append(conflicts, itemConflicts...)
			out = append(out, merged)
		}
	}
	return out, conflicts
}

func mapKeysUnion[T any](maps ...map[generated.ConfigID]T) []generated.ConfigID {
	seen := make(map[generated.ConfigID]struct{})
	for _, m := range maps {
		for k := range m {
			seen[k] = struct{}{}
		}
	}
	out := make([]generated.ConfigID, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func mergeRoute(path string, base, current, draft generated.RouteConfig) (generated.RouteConfig, []Conflict) {
	return mergeThreeWay(path, base, current, draft, mergeRouteConflict)
}

func mergeRouteConflict(path string, base, current, draft generated.RouteConfig) (generated.RouteConfig, []Conflict) {
	out := draft
	var conflicts []Conflict
	val := reflect.ValueOf(&out).Elem()
	baseVal := reflect.ValueOf(base)
	currentVal := reflect.ValueOf(current)
	draftVal := reflect.ValueOf(draft)
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldPath := path + "." + jsonFieldName(field)
		bf := baseVal.Field(i).Interface()
		cf := currentVal.Field(i).Interface()
		df := draftVal.Field(i).Interface()
		if jsonEqual(df, bf) {
			val.Field(i).Set(currentVal.Field(i))
			continue
		}
		if jsonEqual(cf, bf) {
			continue
		}
		if jsonEqual(df, cf) {
			continue
		}
		conflicts = append(conflicts, Conflict{Path: fieldPath, Reason: "field diverged"})
	}
	return out, conflicts
}

func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" || tag == "-" {
		return field.Name
	}
	for i, c := range tag {
		if c == ',' {
			return tag[:i]
		}
	}
	return tag
}

func mergeTarget(path string, base, current, draft generated.MutableTargetCommand) (generated.MutableTargetCommand, []Conflict) {
	return mergeThreeWay(path, base, current, draft, mergeTargetConflict)
}

func mergeTargetConflict(path string, base, current, draft generated.MutableTargetCommand) (generated.MutableTargetCommand, []Conflict) {
	out := draft
	var conflicts []Conflict
	val := reflect.ValueOf(&out).Elem()
	baseVal := reflect.ValueOf(base)
	currentVal := reflect.ValueOf(current)
	draftVal := reflect.ValueOf(draft)
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldPath := path + "." + jsonFieldName(field)
		bf := baseVal.Field(i).Interface()
		cf := currentVal.Field(i).Interface()
		df := draftVal.Field(i).Interface()

		if field.Name == "Capabilities" {
			if setEqual(df, bf) {
				val.Field(i).Set(currentVal.Field(i))
				continue
			}
			if setEqual(cf, bf) {
				continue
			}
			if setEqual(df, cf) {
				continue
			}
			conflicts = append(conflicts, Conflict{Path: fieldPath, Reason: "field diverged"})
			continue
		}

		if jsonEqual(df, bf) {
			val.Field(i).Set(currentVal.Field(i))
			continue
		}
		if jsonEqual(cf, bf) {
			continue
		}
		if jsonEqual(df, cf) {
			continue
		}
		conflicts = append(conflicts, Conflict{Path: fieldPath, Reason: "field diverged"})
	}
	return out, conflicts
}

func mergeConfigCommand(
	baseView, currentView generated.MutableConfigView,
	draft generated.MutableConfigCommand,
	credStates map[generated.ConfigID]*credentialSecretState,
	replaceRequired map[generated.ConfigID]replaceRequiredInfo,
) (generated.MutableConfigCommand, []Conflict, map[generated.ConfigID]*credentialSecretState) {
	base := ViewToCommand(baseView)
	current := ViewToCommand(currentView)

	var conflicts []Conflict
	out := draft

	inst, instConflicts := mergeInstance("instance", base.Instance, current.Instance, draft.Instance)
	out.Instance = inst
	conflicts = append(conflicts, instConflicts...)

	out.Routes, instConflicts = mergeIDCollection("routes", base.Routes, current.Routes, draft.Routes,
		func(r generated.RouteConfig) generated.ConfigID { return r.Id },
		mergeRoute,
	)
	conflicts = append(conflicts, instConflicts...)

	baseTargets := make([]generated.MutableTargetCommand, len(baseView.Targets))
	currentTargets := make([]generated.MutableTargetCommand, len(currentView.Targets))
	for i, t := range baseView.Targets {
		baseTargets[i] = TargetToCommand(t)
	}
	for i, t := range currentView.Targets {
		currentTargets[i] = TargetToCommand(t)
	}
	out.Targets, instConflicts = mergeIDCollection("targets", baseTargets, currentTargets, draft.Targets,
		func(t generated.MutableTargetCommand) generated.ConfigID { return t.Id },
		mergeTarget,
	)
	conflicts = append(conflicts, instConflicts...)

	newStates := make(map[generated.ConfigID]*credentialSecretState)
	baseCreds := indexByID(base.Credentials, func(c generated.MutableCredentialCommand) generated.ConfigID { return c.Id })
	currentCreds := indexByID(current.Credentials, func(c generated.MutableCredentialCommand) generated.ConfigID { return c.Id })
	draftCreds := indexByID(draft.Credentials, func(c generated.MutableCredentialCommand) generated.ConfigID { return c.Id })

	credIDs := mapKeysUnion(baseCreds, currentCreds, draftCreds)
	sort.Slice(credIDs, func(i, j int) bool { return credIDs[i] < credIDs[j] })

	mergedCreds := make([]generated.MutableCredentialCommand, 0, len(draft.Credentials))
	credByID := make(map[generated.ConfigID]generated.MutableCredentialCommand)

	for _, id := range credIDs {
		itemPath := fmt.Sprintf("credentials[%s]", id)
		if _, rr := replaceRequired[id]; rr {
			if d, ok := draftCreds[id]; ok {
				info := replaceRequired[id]
				mergedCreds = append(mergedCreds, applyReplaceRequired(d, info))
				credByID[id] = applyReplaceRequired(d, info)
			}
			continue
		}

		bCmd, bOK := baseCreds[id]
		cCmd, cOK := currentCreds[id]
		dCmd, dOK := draftCreds[id]
		bView, bViewOK := findCredentialView(baseView, id)
		cView, cViewOK := findCredentialView(currentView, id)

		if !bOK && dOK && !cOK {
			mergedCreds = append(mergedCreds, dCmd)
			credByID[id] = dCmd
			continue
		}
		if bOK && !dOK && cOK {
			conflicts = append(conflicts, Conflict{Path: itemPath, Reason: "deleted locally but modified remotely"})
			continue
		}
		if bOK && dOK && !cOK {
			conflicts = append(conflicts, Conflict{Path: itemPath, Reason: "modified locally but deleted remotely"})
			continue
		}
		if !bOK && dOK && cOK {
			if !jsonEqual(dCmd, cCmd) {
				conflicts = append(conflicts, Conflict{Path: itemPath, Reason: "same id different new resources"})
			} else {
				mergedCreds = append(mergedCreds, dCmd)
				credByID[id] = dCmd
			}
			continue
		}
		if !bOK && !dOK && cOK {
			var action generated.CredentialSecretAction
			_ = action.FromCredentialSecretAction0(generated.CredentialSecretAction0{Mode: generated.CredentialSecretActionPreserve})
			c := cCmd
			c.SecretAction = action
			mergedCreds = append(mergedCreds, c)
			credByID[id] = c
			continue
		}

		if !bViewOK || !cViewOK || !dOK {
			continue
		}

		nonSecretMerged, nsConflicts := mergeThreeWay(itemPath, bCmd, cCmd, dCmd, func(p string, b, c, d generated.MutableCredentialCommand) (generated.MutableCredentialCommand, []Conflict) {
			out := d
			if b.Name != d.Name && c.Name != d.Name && b.Name != c.Name {
				return out, []Conflict{{Path: p + ".name", Reason: "field diverged"}}
			}
			if jsonEqual(d.Name, b.Name) {
				out.Name = c.Name
			}
			if b.Provider != d.Provider && c.Provider != d.Provider && b.Provider != c.Provider {
				return out, []Conflict{{Path: p + ".provider", Reason: "field diverged"}}
			}
			if jsonEqual(d.Provider, b.Provider) {
				out.Provider = c.Provider
			}
			return out, nil
		})
		conflicts = append(conflicts, nsConflicts...)

		state := credStates[id]
		merged, secretConflict, newState := mergeCredentialSecret(itemPath, bView, cView, nonSecretMerged, state)
		if secretConflict != nil {
			conflicts = append(conflicts, *secretConflict)
		}
		if newState != nil {
			newStates[id] = newState
		}
		mergedCreds = append(mergedCreds, merged)
		credByID[id] = merged
	}
	out.Credentials = mergedCreds

	// passthrough collections with generic ID merge using json equality
	out.BackendSets, instConflicts = mergeGenericCollection("backend_sets", base.BackendSets, current.BackendSets, draft.BackendSets,
		func(v generated.BackendSetConfig) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, instConflicts...)
	out.ClientProfiles, instConflicts = mergeGenericCollection("client_profiles", base.ClientProfiles, current.ClientProfiles, draft.ClientProfiles,
		func(v generated.MutableClientProfile) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, instConflicts...)
	out.Endpoints, instConflicts = mergeGenericCollection("endpoints", base.Endpoints, current.Endpoints, draft.Endpoints,
		func(v generated.EndpointConfig) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, instConflicts...)
	out.QuotaGroups, instConflicts = mergeGenericCollection("quota_groups", base.QuotaGroups, current.QuotaGroups, draft.QuotaGroups,
		func(v generated.QuotaGroupConfig) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, instConflicts...)
	out.ContentPolicies, instConflicts = mergeGenericCollection("content_policies", base.ContentPolicies, current.ContentPolicies, draft.ContentPolicies,
		func(v generated.ContentPolicyConfig) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, instConflicts...)
	out.CompatibilityTransforms, instConflicts = mergeGenericCollection("compatibility_transforms", base.CompatibilityTransforms, current.CompatibilityTransforms, draft.CompatibilityTransforms,
		func(v generated.CompatibilityTransformConfig) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, instConflicts...)
	out.ModelPolicies, instConflicts = mergeGenericCollection("model_policies", base.ModelPolicies, current.ModelPolicies, draft.ModelPolicies,
		func(v generated.ModelPolicyConfig) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, instConflicts...)
	out.ModelProjections, instConflicts = mergeGenericCollection("model_projections", base.ModelProjections, current.ModelProjections, draft.ModelProjections,
		func(v generated.ModelProjectionConfig) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, instConflicts...)

	baseManaged := managedObjectSlice(base.ManagedObjects)
	currentManaged := managedObjectSlice(current.ManagedObjects)
	draftManaged := managedObjectSlice(draft.ManagedObjects)
	mergedManaged, managedConflicts := mergeGenericCollection("managed_objects", baseManaged, currentManaged, draftManaged,
		func(v generated.ManagedObject) generated.ConfigID { return v.Id })
	conflicts = append(conflicts, managedConflicts...)
	if base.ManagedObjects == nil && current.ManagedObjects == nil && draft.ManagedObjects == nil && len(mergedManaged) == 0 {
		out.ManagedObjects = nil
	} else {
		out.ManagedObjects = &mergedManaged
	}

	return out, conflicts, newStates
}

func managedObjectSlice(objects *[]generated.ManagedObject) []generated.ManagedObject {
	if objects == nil {
		return nil
	}
	return append([]generated.ManagedObject(nil), (*objects)...)
}

func mergeGenericCollection[T any](
	path string,
	base, current, draft []T,
	idFn func(T) generated.ConfigID,
) ([]T, []Conflict) {
	return mergeIDCollection(path, base, current, draft, idFn, func(p string, b, c, d T) (T, []Conflict) {
		return mergeThreeWay(p, b, c, d, mergeGenericObjectConflict[T])
	})
}

func mergeGenericObjectConflict[T any](path string, base, current, draft T) (T, []Conflict) {
	out := draft
	var conflicts []Conflict
	val := reflect.ValueOf(&out).Elem()
	baseVal := reflect.ValueOf(base)
	currentVal := reflect.ValueOf(current)
	draftVal := reflect.ValueOf(draft)
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldPath := path + "." + jsonFieldName(field)
		bf := baseVal.Field(i).Interface()
		cf := currentVal.Field(i).Interface()
		df := draftVal.Field(i).Interface()

		if field.Type.Kind() == reflect.Slice || field.Type.Kind() == reflect.Array {
			if jsonEqual(df, bf) {
				val.Field(i).Set(currentVal.Field(i))
				continue
			}
			if jsonEqual(cf, bf) {
				continue
			}
			if jsonEqual(df, cf) {
				continue
			}
			conflicts = append(conflicts, Conflict{Path: fieldPath, Reason: "field diverged"})
			continue
		}

		if jsonEqual(df, bf) {
			val.Field(i).Set(currentVal.Field(i))
			continue
		}
		if jsonEqual(cf, bf) {
			continue
		}
		if jsonEqual(df, cf) {
			continue
		}
		conflicts = append(conflicts, Conflict{Path: fieldPath, Reason: "field diverged"})
	}
	return out, conflicts
}
