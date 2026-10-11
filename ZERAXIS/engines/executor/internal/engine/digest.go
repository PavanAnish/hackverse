package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ociDigestPattern validates the canonical OCI digest format: algorithm:hex
// For Phase 3 (sha256 only) this is: sha256:<64 lowercase hex chars>
var ociDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// ErrMalformedRepoDigest is returned when a Docker RepoDigest cannot be parsed.
var ErrMalformedRepoDigest = errors.New("malformed RepoDigest: expected format repo@sha256:<hex>")

// ErrInvalidTargetDigest is returned when target_image_digest is not a valid OCI digest.
var ErrInvalidTargetDigest = errors.New("invalid target_image_digest: must be sha256:<64 hex chars>")

// ValidateTargetDigest checks that the Phase 3 authoritative digest stored in target_image_digest
// is a valid OCI digest string. Rejects Docker ImageIDs, tags, and malformed values.
func ValidateTargetDigest(digest string) error {
	if !ociDigestPattern.MatchString(digest) {
		return fmt.Errorf("%w: got %q", ErrInvalidTargetDigest, digest)
	}
	return nil
}

// CanonicalRepoDigest extracts the canonical OCI digest from a Docker RepoDigest string.
//
// Docker RepoDigests have the form:
//
//	registry/repository@sha256:<hex>
//
// This function extracts and returns the "sha256:<hex>" portion.
// It returns ErrMalformedRepoDigest for any string that does not contain exactly one "@"
// followed by a valid OCI digest.
//
// Examples:
//
//	localhost:5000/foo@sha256:abc...  → sha256:abc..., nil
//	sha256:abc...                     → error (no "@" separator)
//	registry/image:tag                → error (tag, not digest reference)
func CanonicalRepoDigest(repoDigest string) (string, error) {
	atIdx := strings.LastIndex(repoDigest, "@")
	if atIdx <= 0 || atIdx == len(repoDigest)-1 {
		return "", fmt.Errorf("%w: %q", ErrMalformedRepoDigest, repoDigest)
	}
	canonical := repoDigest[atIdx+1:]
	if !ociDigestPattern.MatchString(canonical) {
		return "", fmt.Errorf("%w: digest component %q is not a valid OCI digest", ErrMalformedRepoDigest, canonical)
	}
	return canonical, nil
}

// RepoDigestsContain checks whether any of the given Docker RepoDigests contains the
// provided canonical target digest. It extracts the digest component from each entry
// and compares against targetDigest using strict equality.
//
// Returns true only when an exact canonical digest match is found.
// Malformed RepoDigest entries are silently skipped (they cannot authorize).
func RepoDigestsContain(repoDigests []string, targetDigest string) bool {
	for _, rd := range repoDigests {
		canonical, err := CanonicalRepoDigest(rd)
		if err != nil {
			// Malformed entry — cannot authorize, skip safely.
			continue
		}
		if canonical == targetDigest {
			return true
		}
	}
	return false
}
