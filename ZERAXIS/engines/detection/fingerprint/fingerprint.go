package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type FingerprintInput struct {
	RuleID      string            `json:"rule_id"`
	RuleVersion int               `json:"rule_version"`
	AssetID     string            `json:"asset_id"`
	ImageDigest string            `json:"image_digest"`
	Group       map[string]string `json:"group"`
}

// Generate creates a deterministic SHA256 fingerprint from the input parameters.
func Generate(ruleID string, ruleVersion int, assetID, imageDigest string, group map[string]string) string {
	input := FingerprintInput{
		RuleID:      ruleID,
		RuleVersion: ruleVersion,
		AssetID:     assetID,
		ImageDigest: imageDigest,
		Group:       group,
	}
	
	// json.Marshal is deterministic for map keys in standard library (Go 1.8+)
	b, _ := json.Marshal(input)
	
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}
