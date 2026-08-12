package configdraft

import (
	"fmt"
	"sort"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

type Draft struct {
	instanceID       generated.InstanceID
	generation       int64
	revision         string
	baseView         generated.MutableConfigView
	currentView      generated.MutableConfigView
	local            generated.MutableConfigCommand
	credSecrets      map[generated.ConfigID]*credentialSecretState
	replaceReq       map[generated.ConfigID]replaceRequiredInfo
	disconnected     bool
	mutationStatus   generated.ConfigMutationStatus
	inConflict       bool
	conflicts        []Conflict
	conflictGen      int64
	conflictRevision string
}

func Load(snapshot generated.MutableConfigSnapshot) *Draft {
	view := cloneView(snapshot.MutableConfig)
	return &Draft{
		instanceID:     snapshot.InstanceId,
		generation:     snapshot.ActiveGeneration,
		revision:       snapshot.ConfigRevision,
		baseView:       view,
		currentView:    cloneView(view),
		local:          canonicalCommand(ViewToCommand(view)),
		credSecrets:    make(map[generated.ConfigID]*credentialSecretState),
		replaceReq:     make(map[generated.ConfigID]replaceRequiredInfo),
		mutationStatus: generated.ConfigMutationStatusAvailable,
	}
}

func (d *Draft) InstanceID() string { return string(d.instanceID) }
func (d *Draft) Generation() int64  { return d.generation }
func (d *Draft) Revision() string   { return d.revision }

func (d *Draft) BaseView() generated.MutableConfigView {
	return cloneView(d.baseView)
}

func (d *Draft) LocalCommand() generated.MutableConfigCommand {
	cmd := cloneCommand(d.local)
	for i, cred := range cmd.Credentials {
		if info, ok := d.replaceReq[cred.Id]; ok {
			cmd.Credentials[i] = applyReplaceRequired(cred, info)
		}
	}
	return cmd
}

func (d *Draft) Command() (generated.MutableConfigCommand, error) {
	if len(d.replaceReq) > 0 {
		return generated.MutableConfigCommand{}, fmt.Errorf("credential replace required: cannot publish")
	}
	cmd := cloneCommand(d.local)
	out := cmd
	out.Credentials = make([]generated.MutableCredentialCommand, 0, len(cmd.Credentials))
	for _, cred := range cmd.Credentials {
		if _, ok := d.replaceReq[cred.Id]; ok {
			return generated.MutableConfigCommand{}, fmt.Errorf("credential %s requires secret replace", cred.Id)
		}
		mode, _, _, err := secretModeFromAction(cred.SecretAction)
		if err != nil {
			return generated.MutableConfigCommand{}, err
		}
		if mode == SecretReplaceRequired {
			return generated.MutableConfigCommand{}, fmt.Errorf("credential %s requires secret replace", cred.Id)
		}
		out.Credentials = append(out.Credentials, cred)
	}
	return out, nil
}

func (d *Draft) IsDirty() bool {
	return len(d.DirtyDomains()) > 0
}

func (d *Draft) DirtyDomains() []Domain {
	base := canonicalCommand(ViewToCommand(d.baseView))
	local := canonicalCommand(d.LocalCommand())
	var domains []Domain
	if !jsonEqual(local.Instance, base.Instance) {
		domains = append(domains, DomainInstance)
	}
	if !jsonEqual(local.ClientProfiles, base.ClientProfiles) {
		domains = append(domains, DomainProfiles)
	}
	if !jsonEqual(local.Routes, base.Routes) ||
		!jsonEqual(local.BackendSets, base.BackendSets) ||
		!jsonEqual(local.ContentPolicies, base.ContentPolicies) ||
		!jsonEqual(local.CompatibilityTransforms, base.CompatibilityTransforms) ||
		!jsonEqual(local.ModelPolicies, base.ModelPolicies) ||
		!jsonEqual(local.ModelProjections, base.ModelProjections) {
		domains = append(domains, DomainRoutes)
	}
	if !targetsEqual(local.Targets, base.Targets) || !jsonEqual(local.Endpoints, base.Endpoints) || credentialsDirty(local.Credentials, base.Credentials, d.replaceReq, d.credSecrets) {
		domains = append(domains, DomainTargets)
	}
	if !jsonEqual(local.QuotaGroups, base.QuotaGroups) {
		domains = append(domains, DomainQuotas)
	}
	if !jsonEqual(local.ManagedObjects, base.ManagedObjects) {
		domains = append(domains, DomainManaged)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })
	return domains
}

func (d *Draft) DomainDirty(domain Domain) bool {
	for _, dom := range d.DirtyDomains() {
		if dom == domain {
			return true
		}
	}
	return false
}

func credentialsDirty(local, base []generated.MutableCredentialCommand, replaceReq map[generated.ConfigID]replaceRequiredInfo, credSecrets map[generated.ConfigID]*credentialSecretState) bool {
	if len(replaceReq) > 0 || len(credSecrets) > 0 {
		return true
	}
	baseMap := indexByID(base, func(c generated.MutableCredentialCommand) generated.ConfigID { return c.Id })
	localMap := indexByID(local, func(c generated.MutableCredentialCommand) generated.ConfigID { return c.Id })
	if len(baseMap) != len(localMap) {
		return true
	}
	for id, bc := range baseMap {
		lc, ok := localMap[id]
		if !ok {
			return true
		}
		if !jsonEqual(credentialWithoutGeneration(lc), credentialWithoutGeneration(bc)) {
			return true
		}
	}
	return false
}

func targetsEqual(local, base []generated.MutableTargetCommand) bool {
	if len(local) != len(base) {
		return false
	}
	baseMap := indexByID(base, func(t generated.MutableTargetCommand) generated.ConfigID { return t.Id })
	for _, lt := range local {
		bt, ok := baseMap[lt.Id]
		if !ok {
			return false
		}
		ltCopy := lt
		btCopy := bt
		ltCopy.Capabilities = nil
		btCopy.Capabilities = nil
		if !jsonEqual(ltCopy, btCopy) || !setEqual(lt.Capabilities, bt.Capabilities) {
			return false
		}
	}
	return true
}

func (d *Draft) Discard() {
	d.local = canonicalCommand(ViewToCommand(d.baseView))
	d.credSecrets = make(map[generated.ConfigID]*credentialSecretState)
	d.replaceReq = make(map[generated.ConfigID]replaceRequiredInfo)
	d.conflicts = nil
	d.inConflict = false
}

func (d *Draft) SetDisconnected(v bool) { d.disconnected = v }
func (d *Draft) Disconnected() bool     { return d.disconnected }

func (d *Draft) SetMutationStatus(s generated.ConfigMutationStatus) { d.mutationStatus = s }

func (d *Draft) CanPublish() bool {
	if d.disconnected {
		return false
	}
	if d.mutationStatus == generated.ConfigMutationStatusSecretCleanupPending {
		return false
	}
	if d.inConflict {
		return false
	}
	if len(d.replaceReq) > 0 {
		return false
	}
	_, err := d.Command()
	return err == nil
}

func (d *Draft) Mutate(fn func(*generated.MutableConfigCommand)) {
	fn(&d.local)
}

func (d *Draft) SetCredentialReplace(id string, stageID string) {
	credID := generated.ConfigID(id)
	baseGen := 0
	if cv, ok := findCredentialView(d.baseView, credID); ok && cv.Generation != nil {
		baseGen = *cv.Generation
	}
	var action generated.CredentialSecretAction
	_ = action.FromCredentialSecretAction1(generated.CredentialSecretAction1{
		Mode:    generated.CredentialSecretActionReplace,
		StageId: generated.SecretStageID(stageID),
	})
	d.setCredentialAction(credID, action)
	d.credSecrets[credID] = &credentialSecretState{
		mode:           SecretReplace,
		stageID:        generated.SecretStageID(stageID),
		baseGeneration: baseGen,
	}
	delete(d.replaceReq, credID)
}

// ClearCredentialSecretState removes draft-side secret bookkeeping for id
// (replaceReq + credSecrets) without mutating credential rows. Call after a
// successful credential delete so ghosts cannot block CanPublish or sticky-dirty
// DomainTargets.
func (d *Draft) ClearCredentialSecretState(id string) {
	credID := generated.ConfigID(id)
	delete(d.replaceReq, credID)
	delete(d.credSecrets, credID)
}

func (d *Draft) setCredentialAction(id generated.ConfigID, action generated.CredentialSecretAction) {
	found := false
	for i, c := range d.local.Credentials {
		if c.Id == id {
			d.local.Credentials[i].SecretAction = action
			found = true
			break
		}
	}
	if !found {
		if cv, ok := findCredentialView(d.baseView, id); ok {
			cmd := credentialViewToCommand(cv)
			cmd.SecretAction = action
			d.local.Credentials = append(d.local.Credentials, cmd)
		}
	}
}

func (d *Draft) MarkReplaceRequired(credentialID string) {
	id := generated.ConfigID(credentialID)
	var cred generated.MutableCredentialCommand
	found := false
	for _, c := range d.local.Credentials {
		if c.Id == id {
			cred = c
			found = true
			break
		}
	}
	if !found {
		if cv, ok := findCredentialView(d.baseView, id); ok {
			cred = credentialViewToCommand(cv)
			found = true
		}
	}
	if !found {
		return
	}
	d.replaceReq[id] = replaceRequiredInfo{
		name:     cred.Name,
		provider: cred.Provider,
	}
	delete(d.credSecrets, id)
}

func (d *Draft) BeginConflict(current generated.MutableConfigSnapshot) {
	d.currentView = cloneView(current.MutableConfig)
	d.conflictGen = current.ActiveGeneration
	d.conflictRevision = current.ConfigRevision
	d.inConflict = true
	d.conflicts = nil
}

func (d *Draft) ReplaceRequiredIDs() []string {
	if len(d.replaceReq) == 0 {
		return nil
	}
	ids := make([]string, 0, len(d.replaceReq))
	for id := range d.replaceReq {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	return ids
}

func (d *Draft) InConflict() bool { return d.inConflict }

func (d *Draft) Reapply() []Conflict {
	merged, conflicts, newStates := mergeConfigCommand(
		d.baseView,
		d.currentView,
		d.local,
		d.credSecrets,
		d.replaceReq,
	)
	d.local = merged
	d.credSecrets = newStates
	d.conflicts = conflicts
	d.inConflict = len(conflicts) > 0

	if len(conflicts) == 0 {
		d.baseView = cloneView(d.currentView)
		if d.conflictGen > 0 {
			d.generation = d.conflictGen
		}
		if d.conflictRevision != "" {
			d.revision = d.conflictRevision
		}
		d.inConflict = false
		d.conflictGen = 0
		d.conflictRevision = ""
	}
	return append([]Conflict(nil), conflicts...)
}

func (d *Draft) Conflicts() []Conflict {
	return append([]Conflict(nil), d.conflicts...)
}

func (d *Draft) AcceptCurrent() {
	d.baseView = cloneView(d.currentView)
	d.local = canonicalCommand(ViewToCommand(d.currentView))
	d.credSecrets = make(map[generated.ConfigID]*credentialSecretState)
	d.replaceReq = make(map[generated.ConfigID]replaceRequiredInfo)
	d.conflicts = nil
	d.inConflict = false
	if d.conflictGen > 0 {
		d.generation = d.conflictGen
	}
	if d.conflictRevision != "" {
		d.revision = d.conflictRevision
	}
	d.conflictGen = 0
	d.conflictRevision = ""
}
