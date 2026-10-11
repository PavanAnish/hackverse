package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Evaluator defines the interface to Shield's OPA/Cache evaluation logic
type Evaluator interface {
	Evaluate(ctx context.Context, fingerprint string) (allowed bool, reason string, err error)
}

type WebhookHandler struct {
	evaluator Evaluator
}

func NewWebhookHandler(evaluator Evaluator) *WebhookHandler {
	return &WebhookHandler{evaluator: evaluator}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Apply strict 2-second timeout (Fail-Closed)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if r.Method != http.MethodPost {
		http.Error(w, "invalid method", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	review, err := h.parseAdmissionReview(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not parse admission review: %v", err), http.StatusBadRequest)
		return
	}

	if review.Request == nil {
		http.Error(w, "admission review request is empty", http.StatusBadRequest)
		return
	}

	var pod corev1.Pod
	if err := json.Unmarshal(review.Request.Object.Raw, &pod); err != nil {
		resp := h.buildAdmissionResponse(string(review.Request.UID), false, fmt.Sprintf("invalid pod object: %v", err))
		h.writeResponse(w, resp)
		return
	}

	digests, err := h.extractImageDigests(&pod)
	if err != nil {
		resp := h.buildAdmissionResponse(string(review.Request.UID), false, err.Error())
		h.writeResponse(w, resp)
		return
	}

	allowed, reason, err := h.evaluateImages(ctx, digests)
	
	// Fail-closed on error (including timeout)
	if err != nil {
		allowed = false
		reason = fmt.Sprintf("evaluation error: %v", err)
	}

	resp := h.buildAdmissionResponse(string(review.Request.UID), allowed, reason)
	h.writeResponse(w, resp)
}

func (h *WebhookHandler) parseAdmissionReview(body []byte) (*admissionv1.AdmissionReview, error) {
	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		return nil, err
	}
	if review.APIVersion == "" || review.Kind == "" {
		return nil, fmt.Errorf("missing APIVersion or Kind")
	}
	return &review, nil
}

func (h *WebhookHandler) extractImageDigests(pod *corev1.Pod) ([]string, error) {
	var images []string
	
	for _, c := range pod.Spec.InitContainers {
		images = append(images, c.Image)
	}
	for _, c := range pod.Spec.Containers {
		images = append(images, c.Image)
	}
	for _, c := range pod.Spec.EphemeralContainers {
		images = append(images, c.Image)
	}
	
	if len(images) == 0 {
		return nil, fmt.Errorf("no images found in pod specification")
	}
	
	return images, nil
}

func (h *WebhookHandler) evaluateImages(ctx context.Context, digests []string) (bool, string, error) {
	for _, digest := range digests {
		allowed, reason, err := h.evaluator.Evaluate(ctx, digest)
		if err != nil {
			return false, "", err
		}
		if !allowed {
			return false, fmt.Sprintf("image %s blocked: %s", digest, reason), nil
		}
	}
	return true, "", nil
}

func (h *WebhookHandler) buildAdmissionResponse(uid string, allowed bool, reason string) *admissionv1.AdmissionResponse {
	resp := &admissionv1.AdmissionResponse{
		UID:     types.UID(uid),
		Allowed: allowed,
	}
	
	if !allowed {
		resp.Result = &metav1.Status{
			Status:  "Failure",
			Message: reason,
		}
	}
	
	return resp
}

func (h *WebhookHandler) writeResponse(w http.ResponseWriter, resp *admissionv1.AdmissionResponse) {
	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: resp,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(review)
}
