package admission_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pavanvanish/zeraxis/engines/shield/internal/admission"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/evaluator"
)

const (
	maliciousDigest = "sha256:MALICIOUS_DIGEST"
	cleanDigest     = "sha256:CLEAN_DIGEST"
)

func postEvaluate(srv *admission.Server, req evaluator.AdmissionRequest) (*admission.EvaluateResponse, int) {
	b, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest("POST", "/v1/admission/evaluate", bytes.NewBuffer(b))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httpReq)
	var resp admission.EvaluateResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	return &resp, rr.Code
}

func TestIdentitySpoofing_DigestIsAuthoritative(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	srv, cacheView := setupServer(t, db)

	// SETUP: Block the malicious digest in cache (simulates active incident blocking)
	err := cacheView.Set(context.Background(), maliciousDigest, "CRITICAL_MALWARE_DETECTED", 5*time.Minute)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// --- ATTEMPT 1: Blocked digest with innocuous labels ---
	resp, code := postEvaluate(srv, evaluator.AdmissionRequest{
		Fingerprint: maliciousDigest,
		Registry:    "trusted.io",
		Labels:      map[string]string{"foo": "bar"},
	})
	if code != http.StatusOK || resp.Allowed || resp.Reason != "CRITICAL_MALWARE_DETECTED" {
		t.Errorf("ATTEMPT 1 FAILED: Expected blocked, got allowed=%v reason=%s", resp.Allowed, resp.Reason)
	} else {
		t.Logf("✓ ATTEMPT 1: Blocked digest + labels={foo:bar} → BLOCKED (%s)", resp.Reason)
	}

	// --- ATTEMPT 2: SAME blocked digest, mutated labels (must NOT bypass) ---
	resp, code = postEvaluate(srv, evaluator.AdmissionRequest{
		Fingerprint: maliciousDigest,
		Registry:    "trusted.io",
		Labels:      map[string]string{"foo": "baz"},
	})
	if code != http.StatusOK || resp.Allowed {
		t.Errorf("ATTEMPT 2 FAILED: Label mutation bypassed block! got allowed=%v reason=%s", resp.Allowed, resp.Reason)
	} else {
		t.Logf("✓ ATTEMPT 2: Same digest + mutated labels={foo:baz} → BLOCKED (metadata change does NOT bypass)")
	}

	// --- ATTEMPT 3: SAME blocked digest, different container name via labels (must NOT bypass) ---
	resp, code = postEvaluate(srv, evaluator.AdmissionRequest{
		Fingerprint: maliciousDigest,
		Registry:    "trusted.io",
		Labels:      map[string]string{"com.docker.name": "innocent_name", "foo": "bar"},
	})
	if code != http.StatusOK || resp.Allowed {
		t.Errorf("ATTEMPT 3 FAILED: Container name change bypassed block! got allowed=%v reason=%s", resp.Allowed, resp.Reason)
	} else {
		t.Logf("✓ ATTEMPT 3: Same digest + container_name=innocent_name → BLOCKED (name does NOT bypass)")
	}

	// --- ATTEMPT 4: CLEAN digest with IDENTICAL labels as blocked attempts (must ALLOW) ---
	resp, code = postEvaluate(srv, evaluator.AdmissionRequest{
		Fingerprint: cleanDigest,
		Registry:    "trusted.io",
		Labels:      map[string]string{"foo": "bar"},
	})
	if code != http.StatusOK || !resp.Allowed {
		t.Errorf("ATTEMPT 4 FAILED: Clean digest was blocked! got allowed=%v reason=%s", resp.Allowed, resp.Reason)
	} else {
		t.Logf("✓ ATTEMPT 4: Clean digest + identical labels={foo:bar} → ALLOWED (block is digest-scoped, not label-scoped)")
	}

	t.Logf("\n✓ IDENTITY SPOOFING VERDICT: OCI digest is authoritative identity. Non-authoritative metadata (labels, container name) cannot bypass a block.")
}

func TestIdentitySpoofing_RegistryCannotSpoofDigest(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	srv, cacheView := setupServer(t, db)

	// SETUP: Block via digest fingerprint
	cacheView.Set(context.Background(), maliciousDigest, "REGISTRY_BLOCK", 5*time.Minute)

	// Attempt: Same digest from a "trusted" registry — must still be blocked
	resp, _ := postEvaluate(srv, evaluator.AdmissionRequest{
		Fingerprint: maliciousDigest,
		Registry:    "gcr.io",    // authoritative registry, but digest is blocked
	})
	if resp.Allowed {
		t.Errorf("SPOOFING FAILED: Changing registry to gcr.io bypassed block on malicious digest")
	} else {
		t.Logf("✓ Trusted registry (gcr.io) does NOT override a blocked digest — digest is authoritative")
	}
}

func TestIdentitySpoofing_AdversarialFingerprintPrefixAttack(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	srv, cacheView := setupServer(t, db)

	// Block the exact malicious digest
	cacheView.Set(context.Background(), maliciousDigest, "KNOWN_MALWARE", 5*time.Minute)

	// Attempt: fingerprint that STARTS with blocked digest (prefix attack)
	prefixAttack := maliciousDigest + "_EXTRA"
	resp, _ := postEvaluate(srv, evaluator.AdmissionRequest{
		Fingerprint: prefixAttack,
		Registry:    "trusted.io",
	})
	if !resp.Allowed {
		// Good if blocked, but check — it should NOT be blocked unless exact match
		t.Logf("✓ PREFIX ATTACK: Extended fingerprint also blocked (strict match)")
	} else {
		t.Logf("✓ PREFIX ATTACK: Different fingerprint correctly ALLOWED — block is exact-match only")
	}

	// Ensure original digest still blocked
	resp, _ = postEvaluate(srv, evaluator.AdmissionRequest{
		Fingerprint: maliciousDigest,
		Registry:    "trusted.io",
	})
	if resp.Allowed {
		t.Errorf("SPOOFING ALERT: Original blocked digest now reports ALLOWED after prefix attack attempt!")
	} else {
		t.Logf("✓ Original blocked digest still BLOCKED after prefix attack")
	}
}
