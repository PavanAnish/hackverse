package breakglass

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type HMACValidator struct{}

func NewHMACValidator() *HMACValidator {
	return &HMACValidator{}
}

func (v *HMACValidator) GenerateSignature(requestID, approverID, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	// Using a deterministic payload structure for the signature
	mac.Write([]byte(fmt.Sprintf("%s|%s", requestID, approverID)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (v *HMACValidator) ValidateSignature(requestID, approverID, providedSignature, secret string) bool {
	expected := v.GenerateSignature(requestID, approverID, secret)
	return hmac.Equal([]byte(expected), []byte(providedSignature))
}
