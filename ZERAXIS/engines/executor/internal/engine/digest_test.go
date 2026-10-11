package engine

import (
	"fmt"
	"testing"
)

func TestCanonicalRepoDigest_Valid(t *testing.T) {
	rd := "localhost:5000/foo@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	got, err := CanonicalRepoDigest(rd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	want := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCanonicalRepoDigest_MissingAt(t *testing.T) {
	// A bare digest (Docker ImageID format) must be rejected.
	_, err := CanonicalRepoDigest("sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if err == nil {
		t.Fatal("expected error for missing '@' separator, got nil")
	}
}

func TestCanonicalRepoDigest_Tag(t *testing.T) {
	// Tag-only references must be rejected.
	_, err := CanonicalRepoDigest("localhost:5000/foo:latest")
	if err == nil {
		t.Fatal("expected error for tag reference, got nil")
	}
}

func TestCanonicalRepoDigest_Malformed(t *testing.T) {
	cases := []string{
		"registry/image@notadigest",
		"registry/image@sha256:tooshort",
		"@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"",
	}
	for _, c := range cases {
		if _, err := CanonicalRepoDigest(c); err == nil {
			t.Errorf("expected error for malformed input %q, got nil", c)
		}
	}
}

func TestValidateTargetDigest_Valid(t *testing.T) {
	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if err := ValidateTargetDigest(digest); err != nil {
		t.Fatalf("expected valid digest to pass, got: %v", err)
	}
}

func TestValidateTargetDigest_ImageID_Rejected(t *testing.T) {
	// Docker ImageIDs look like sha256:<hex> but from local cache — same format.
	// The rule is: the SOURCE must be Phase 3 registry, not that the format differs.
	// We can't distinguish by format alone, but we MUST still validate format is correct.
	// If someone attempts to store an ImageID as target_image_digest, the format
	// is identical — the distinction is enforced by the RepoDigest comparison at runtime.
	// This test documents that format validation alone does not prevent ImageID misuse.
	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if err := ValidateTargetDigest(digest); err != nil {
		t.Fatalf("format validation: expected pass for sha256 hex, got: %v", err)
	}
	// The authoritative enforcement is in RepoDigestsContain — if the stored digest
	// matches only ImageID and not any RepoDigest entry, execution is denied.
}

func TestValidateTargetDigest_Invalid(t *testing.T) {
	cases := []string{
		"",
		"sha256:tooshort",
		"md5:abcdef",
		"alpine:latest",
		"localhost:5000/foo@sha256:abc",
	}
	for _, c := range cases {
		if err := ValidateTargetDigest(c); err == nil {
			t.Errorf("expected invalid digest %q to fail, got nil", c)
		}
	}
}

func TestRepoDigestsContain_Match(t *testing.T) {
	target := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	repoDigests := []string{
		fmt.Sprintf("localhost:5000/myimage@%s", target),
	}
	if !RepoDigestsContain(repoDigests, target) {
		t.Fatal("expected match, got false")
	}
}

func TestRepoDigestsContain_Mismatch(t *testing.T) {
	target := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	other := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	repoDigests := []string{
		fmt.Sprintf("localhost:5000/myimage@%s", other),
	}
	if RepoDigestsContain(repoDigests, target) {
		t.Fatal("expected no match, got true")
	}
}

func TestRepoDigestsContain_ImageID_Cannot_Authorize(t *testing.T) {
	// An ImageID looks like sha256:<hex> and cannot appear in RepoDigests
	// in the "repo@digest" format. If someone passes it bare, CanonicalRepoDigest
	// will reject it (no "@" separator).
	imageID := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	repoDigests := []string{imageID} // bare ImageID, not "repo@digest" format
	if RepoDigestsContain(repoDigests, imageID) {
		t.Fatal("bare ImageID must not authorize — expected false")
	}
}

func TestRepoDigestsContain_MalformedEntries_Skipped(t *testing.T) {
	target := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	repoDigests := []string{
		"notvalid",
		"registry/image:tag",
		"registry/image@notadigest",
	}
	if RepoDigestsContain(repoDigests, target) {
		t.Fatal("malformed entries must not authorize")
	}
}

func TestRepoDigestsContain_Empty(t *testing.T) {
	target := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if RepoDigestsContain(nil, target) {
		t.Fatal("empty RepoDigests must not authorize")
	}
}
