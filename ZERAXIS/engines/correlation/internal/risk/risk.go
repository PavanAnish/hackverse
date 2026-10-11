package risk

import (
	"encoding/json"
	"math"
	"time"

	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
)

const ModelVersion = "1.0"

// CalculateRisk evaluates the Phase 6.3 deterministic risk model.
// Returns static, dynamic, combined risk, level, and confidence.
// Handles decay based on `calcTime` and the incident's `lastSeen`.
func CalculateRisk(
	inc *models.Incident,
	cveSeverity string,
	policyDecision string,
	calcTime time.Time,
	findings []models.FindingRef,
) {
	// 1. Static Risk
	staticRisk := calculateStaticRisk(cveSeverity, policyDecision)

	// 2. Dynamic Risk & Attack Chain State
	// Findings define the max severity and attack chain progression
	maxFindingSev := "INFO"
	for _, f := range findings {
		maxFindingSev = models.MaxSeverity(maxFindingSev, f.Severity)
	}

	attackState := evaluateAttackChain(findings)
	baseDynamic := calculateBaseDynamicRisk(maxFindingSev, len(findings), attackState)

	// Decay dynamic risk based on inactivity
	dynamicRisk := decayDynamicRisk(baseDynamic, inc.LastSeen, calcTime)

	// 3. Combined Risk
	combinedRisk := int(math.Round(math.Min(100.0, float64(staticRisk)*0.4+float64(dynamicRisk)*0.6)))
	
	// Override: If dynamic runtime risk alone is CRITICAL (>=90) or HIGH, it overrides the static risk cap.
	if dynamicRisk >= 90 && combinedRisk < dynamicRisk {
		combinedRisk = dynamicRisk
	}

	// 4. Risk Level
	riskLevel := determineRiskLevel(combinedRisk)

	// 5. Confidence
	confidence := calculateConfidence(findings, attackState)

	// Populate incident fields
	inc.StaticRisk = &staticRisk
	inc.DynamicRisk = &dynamicRisk
	inc.CombinedRisk = &combinedRisk
	inc.RiskLevel = &riskLevel
	inc.Confidence = &confidence
	inc.CalculatedAt = &calcTime
	inc.AttackChainState = &attackState.State

	v := ModelVersion
	inc.RiskModelVersion = &v

	b, _ := json.Marshal(attackState.Evidence)
	inc.AttackChainEvidence = b
}

func calculateStaticRisk(cveSev string, policy string) int {
	// BaseCVSS_Severity
	baseScore := 0
	switch cveSev {
	case "CRITICAL":
		baseScore = 80
	case "HIGH":
		baseScore = 60
	case "MEDIUM":
		baseScore = 40
	case "LOW":
		baseScore = 20
	case "INFO":
		baseScore = 0
	case "UNKNOWN", "":
		baseScore = 0
	}

	// AssetCriticality is UNKNOWN = 1.0 (for now, lacking Discovery attribute fetch)
	score := baseScore

	// PolicyBlockModifier
	if policy == "BLOCK" {
		score += 20
	}

	if score > 100 {
		return 100
	}
	return score
}

func calculateBaseDynamicRisk(maxSev string, findingCount int, ac AttackChainStatus) int {
	baseScore := 0
	switch maxSev {
	case "CRITICAL":
		baseScore = 80
	case "HIGH":
		baseScore = 60
	case "MEDIUM":
		baseScore = 40
	case "LOW":
		baseScore = 20
	case "INFO":
		baseScore = 0
	}

	// TemporalDensityMultiplier: 1.0 + (MIN(20, FindingCount) * 0.05). Maximum 2.0x.
	count := findingCount
	if count > 20 {
		count = 20
	}
	density := 1.0 + (float64(count) * 0.05)

	// AttackChainModifier
	modifier := 1.0
	switch ac.State {
	case "IMPACT":
		modifier = 2.0
	case "NETWORK_ACTIVITY":
		modifier = 1.5
	case "EXECUTION":
		modifier = 1.25
	case "EXPOSURE", "UNKNOWN":
		modifier = 1.0
	}

	res := float64(baseScore) * density * modifier
	if res > 100.0 {
		return 100
	}
	return int(math.Round(res))
}

func decayDynamicRisk(baseDynamic int, lastSeen time.Time, calcTime time.Time) int {
	silence := calcTime.Sub(lastSeen).Hours()

	if silence < 0 {
		silence = 0 // handle clock skew
	}

	// Phase 6.3 Prompt strictly mandates: "Dynamic risk decays linearly to zero after 24 HOURS of silence."
	if silence >= 24.0 {
		return 0
	}

	decayFactor := 1.0 - (silence / 24.0)
	val := float64(baseDynamic) * decayFactor

	res := int(math.Round(val))
	if res < 0 {
		return 0
	}
	return res
}

func determineRiskLevel(score int) string {
	if score >= 90 {
		return "CRITICAL"
	} else if score >= 70 {
		return "HIGH"
	} else if score >= 40 {
		return "MEDIUM"
	} else if score >= 20 {
		return "LOW"
	}
	return "INFO"
}

// AttackChainStatus represents the calculated state machine outputs
type AttackChainStatus struct {
	State    string
	Evidence map[string]string // Stage -> FindingID/Reason
}

// evaluateAttackChain strictly determines stage progression based on evidence.
func evaluateAttackChain(findings []models.FindingRef) AttackChainStatus {
	ac := AttackChainStatus{
		State:    "UNKNOWN",
		Evidence: make(map[string]string),
	}

	hasExposure := false
	hasExecution := false
	hasNetwork := false
	hasImpact := false

	for _, f := range findings {
		// Map finding rule types or categories to attack chain stages.
		// For deterministic tests and generic Phase 6, we rely on the RuleID or EventType heuristics.
		// In actual Phase 5, rules dictate behavior. We infer from string matching for correlation abstraction.

		// Note: Phase 6.3 prompt requires explicit evidence.
		// "EXECUTION: runtime execution evidence exists."
		// "NETWORK_ACTIVITY: qualifying network activity exists."

		// If rule_id explicitly marks impact or severity is Critical with specific rules.
		if f.RuleID == "impact_observed" || f.RuleID == "file_manipulation" || f.RuleID == "db_corruption" || f.RuleID == "rule-sensitive-file-write" {
			hasImpact = true
			ac.Evidence["IMPACT"] = f.FindingID
		} else if f.RuleID == "network_connection" || f.RuleID == "unauthorized_egress" || f.RuleID == "suspicious_network" {
			hasNetwork = true
			ac.Evidence["NETWORK_ACTIVITY"] = f.FindingID
		} else if f.RuleID == "process_execution" || f.RuleID == "shell_spawned" || f.RuleID == "malicious_binary" || f.RuleID == "rule-unexpected-shell" {
			hasExecution = true
			ac.Evidence["EXECUTION"] = f.FindingID
		} else if f.RuleID == "vulnerability_found" || f.RuleID == "policy_block" {
			hasExposure = true
			ac.Evidence["EXPOSURE"] = f.FindingID
		} else {
			// Generic fallback mapping based on standard rule patterns
			hasExposure = true // Any generic finding is at least an exposure
			if ac.Evidence["EXPOSURE"] == "" {
				ac.Evidence["EXPOSURE"] = f.FindingID
			}
		}
	}

	// State machine progression requires evidence for the specific stage.
	// We advance state strictly based on highest achieved state with evidence.
	if hasImpact {
		ac.State = "IMPACT"
	} else if hasNetwork {
		ac.State = "NETWORK_ACTIVITY"
	} else if hasExecution {
		ac.State = "EXECUTION"
	} else if hasExposure {
		ac.State = "EXPOSURE"
	}

	return ac
}

func calculateConfidence(findings []models.FindingRef, ac AttackChainStatus) int {
	// Baseline confidence is 100.
	// Modify by evidence strength. For phase 6, finding severity acts as a proxy for evidence strength
	// (CRITICAL/HIGH = STRONG, MEDIUM = MEDIUM, LOW = WEAK, INFO = UNKNOWN)
	// unless explicitly defined by rule logic.

	confidence := 100.0

	maxStrength := "UNKNOWN"
	for _, f := range findings {
		s := "UNKNOWN"
		if f.Severity == "CRITICAL" || f.Severity == "HIGH" {
			s = "STRONG"
		} else if f.Severity == "MEDIUM" {
			s = "MEDIUM"
		} else if f.Severity == "LOW" {
			s = "WEAK"
		}

		if s == "STRONG" {
			maxStrength = "STRONG"
		} else if s == "MEDIUM" && maxStrength != "STRONG" {
			maxStrength = "MEDIUM"
		} else if s == "WEAK" && maxStrength == "UNKNOWN" {
			maxStrength = "WEAK"
		}
	}

	switch maxStrength {
	case "MEDIUM":
		confidence *= 0.7
	case "WEAK":
		confidence *= 0.4
	case "UNKNOWN":
		confidence *= 0.1
	}

	return int(math.Round(confidence))
}
