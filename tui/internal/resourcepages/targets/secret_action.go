package targets

// SecretActionMode is the closed UI control for credential secret actions.
// Wire encoding uses generated CredentialSecretAction0/1/2 only.
type SecretActionMode string

const (
	SecretActionPreserve SecretActionMode = "preserve"
	SecretActionReplace  SecretActionMode = "replace"
	SecretActionExternal SecretActionMode = "external_ref"
)

// OperationUnknownOutcome models publish recovery after operation_unknown.
type OperationUnknownOutcome int

const (
	OperationUnknownSuccess OperationUnknownOutcome = iota
	OperationUnknownUnchanged
	OperationUnknownConflict
)
