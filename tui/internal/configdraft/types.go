package configdraft

type Domain string

const (
	DomainInstance Domain = "instance"
	DomainProfiles Domain = "profiles"
	DomainRoutes   Domain = "routes"
	DomainTargets  Domain = "targets"
	DomainQuotas   Domain = "quotas"
	DomainManaged  Domain = "managed_objects"
)

type Conflict struct {
	Path   string
	Reason string
}

type SecretMode string

const (
	SecretPreserve        SecretMode = "preserve"
	SecretReplace         SecretMode = "replace"
	SecretExternalRef     SecretMode = "external_ref"
	SecretReplaceRequired SecretMode = "replace_required"
)
