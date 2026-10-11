package constants

// Failure Taxonomy Constants
// Changing these prefixes is a behavioral/API breaking change.
// These are matched deterministically against execution error logs.

const (
	PrefixTargetMissing          = "No such container"
	PrefixDockerUnavailable      = "docker daemon"
	PrefixDockerConnection       = "Cannot connect to the Docker daemon"
	PrefixDockerInspectGeneric   = "Docker inspect failed:"
	PrefixTargetIdentityMismatch = "container ID mismatch:"
	PrefixImageDigestMismatch    = "image digest verification failed:"
	PrefixPostconditionNotMet    = "container state" // "container state ... is not terminal"

	// Actions
	ActionInvestigateProcess = "INVESTIGATE_PROCESS"
	ActionQuarantineAsset    = "QUARANTINE_ASSET"
	ActionNoRecovery         = "NO_RECOVERY"
)
