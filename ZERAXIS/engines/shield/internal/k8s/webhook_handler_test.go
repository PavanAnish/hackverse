package k8s_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pavanvanish/zeraxis/engines/shield/internal/k8s"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// MockEvaluator for testing
type MockEvaluator struct {
	blockedImages map[string]string
	delay         time.Duration
}

func (m *MockEvaluator) Evaluate(ctx context.Context, fingerprint string) (bool, string, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return false, "", ctx.Err()
		}
	}
	
	if reason, ok := m.blockedImages[fingerprint]; ok {
		return false, reason, nil
	}
	return true, "", nil
}

func buildReview(images []string, uid string) []byte {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{},
	}
	for _, img := range images {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Image: img})
	}
	podBytes, _ := json.Marshal(pod)

	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Object: runtime.RawExtension{
				Raw: podBytes,
			},
		},
	}
	b, _ := json.Marshal(review)
	return b
}

func TestWebhookHandler_PodCreate_Allowed(t *testing.T) {
	eval := &MockEvaluator{blockedImages: make(map[string]string)}
	handler := k8s.NewWebhookHandler(eval)

	req := httptest.NewRequest("POST", "/v1/k8s/validate", bytes.NewReader(buildReview([]string{"nginx:latest"}, "uid-1")))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var resp admissionv1.AdmissionReview
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.Response == nil || !resp.Response.Allowed {
		t.Errorf("Expected pod to be allowed")
	}
}

func TestWebhookHandler_PodCreate_Blocked(t *testing.T) {
	eval := &MockEvaluator{blockedImages: map[string]string{"malicious:latest": "CRITICAL_BLOCK"}}
	handler := k8s.NewWebhookHandler(eval)

	req := httptest.NewRequest("POST", "/v1/k8s/validate", bytes.NewReader(buildReview([]string{"malicious:latest"}, "uid-2")))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var resp admissionv1.AdmissionReview
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.Response == nil || resp.Response.Allowed {
		t.Fatalf("Expected pod to be blocked")
	}
	if !strings.Contains(resp.Response.Result.Message, "CRITICAL_BLOCK") {
		t.Errorf("Expected block reason in message")
	}
}

func TestWebhookHandler_MultipleContainers(t *testing.T) {
	eval := &MockEvaluator{blockedImages: map[string]string{"malicious:latest": "CRITICAL_BLOCK"}}
	handler := k8s.NewWebhookHandler(eval)

	var images []string
	for i := 0; i < 9; i++ {
		images = append(images, "safe:latest")
	}
	images = append(images, "malicious:latest") // 10th container is malicious

	req := httptest.NewRequest("POST", "/v1/k8s/validate", bytes.NewReader(buildReview(images, "uid-3")))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var resp admissionv1.AdmissionReview
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.Response == nil || resp.Response.Allowed {
		t.Errorf("Expected multi-container pod with 1 malicious image to be blocked")
	}
}

func TestWebhookHandler_Timeout_2s(t *testing.T) {
	// Simulator takes 2.5s, webhook context should timeout at 2s
	eval := &MockEvaluator{delay: 2500 * time.Millisecond}
	handler := k8s.NewWebhookHandler(eval)

	req := httptest.NewRequest("POST", "/v1/k8s/validate", bytes.NewReader(buildReview([]string{"nginx:latest"}, "uid-4")))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var resp admissionv1.AdmissionReview
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.Response == nil || resp.Response.Allowed {
		t.Fatalf("Expected fail-closed timeout block")
	}
	if !strings.Contains(resp.Response.Result.Message, "context deadline exceeded") {
		t.Errorf("Expected timeout message, got %s", resp.Response.Result.Message)
	}
}

func TestWebhookHandler_MalformedRequest(t *testing.T) {
	eval := &MockEvaluator{}
	handler := k8s.NewWebhookHandler(eval)

	req := httptest.NewRequest("POST", "/v1/k8s/validate", bytes.NewReader([]byte("{invalid json")))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request, got %d", rr.Code)
	}
}
