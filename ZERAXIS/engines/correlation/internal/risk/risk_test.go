package risk

import (
	"testing"
	"time"

	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
)

func TestCalculateRisk_CriticalVuln_NoRuntime(t *testing.T) {
	inc := &models.Incident{LastSeen: time.Now()}
	findings := []models.FindingRef{}

	CalculateRisk(inc, "CRITICAL", "UNKNOWN", time.Now(), findings)

	if *inc.StaticRisk != 80 {
		t.Errorf("Expected StaticRisk 80, got %d", *inc.StaticRisk)
	}
	if *inc.DynamicRisk != 0 {
		t.Errorf("Expected DynamicRisk 0, got %d", *inc.DynamicRisk)
	}
	if *inc.CombinedRisk != 32 { // (80*0.4) + (0*0.6) = 32
		t.Errorf("Expected CombinedRisk 32, got %d", *inc.CombinedRisk)
	}
	if *inc.AttackChainState != "UNKNOWN" {
		t.Errorf("Expected state UNKNOWN, got %s", *inc.AttackChainState)
	}
}

func TestCalculateRisk_CriticalVuln_Execution(t *testing.T) {
	inc := &models.Incident{LastSeen: time.Now()}
	findings := []models.FindingRef{
		{FindingID: "f1", RuleID: "process_execution", Severity: "HIGH"},
	}

	CalculateRisk(inc, "CRITICAL", "UNKNOWN", time.Now(), findings)

	// BaseDynamic = 60 (HIGH) * 1.05 (1 finding) * 1.25 (EXECUTION) = 78.75 -> 79
	if *inc.DynamicRisk != 79 {
		t.Errorf("Expected DynamicRisk 79, got %d", *inc.DynamicRisk)
	}
	// Static = 80
	// Combined = (80*0.4=32) + (79*0.6=47.4) = 79.4 -> 79
	if *inc.CombinedRisk != 79 {
		t.Errorf("Expected CombinedRisk 79, got %d", *inc.CombinedRisk)
	}
	if *inc.AttackChainState != "EXECUTION" {
		t.Errorf("Expected state EXECUTION, got %s", *inc.AttackChainState)
	}
}

func TestCalculateRisk_HighVuln_Execution(t *testing.T) {
	inc := &models.Incident{LastSeen: time.Now()}
	findings := []models.FindingRef{
		{FindingID: "f1", RuleID: "shell_spawned", Severity: "MEDIUM"},
	}

	CalculateRisk(inc, "HIGH", "UNKNOWN", time.Now(), findings)

	// BaseDynamic = 40 (MEDIUM) * 1.05 * 1.25 = 52.5 -> 53
	// Static = 60
	// Combined = (60*0.4=24) + (53*0.6=31.8) = 55.8 -> 56
	if *inc.StaticRisk != 60 {
		t.Errorf("Expected StaticRisk 60, got %d", *inc.StaticRisk)
	}
	if *inc.DynamicRisk != 53 {
		t.Errorf("Expected DynamicRisk 53, got %d", *inc.DynamicRisk)
	}
	if *inc.CombinedRisk != 56 {
		t.Errorf("Expected CombinedRisk 56, got %d", *inc.CombinedRisk)
	}
}

func TestCalculateRisk_WeakEvidence_SuspiciousNetwork(t *testing.T) {
	inc := &models.Incident{LastSeen: time.Now()}
	findings := []models.FindingRef{
		{FindingID: "f1", RuleID: "suspicious_network", Severity: "LOW"}, // WEAK evidence
	}

	CalculateRisk(inc, "INFO", "UNKNOWN", time.Now(), findings)

	// BaseDynamic = 20 (LOW) * 1.05 * 1.5 (NETWORK) = 31.5 -> 32
	// Static = 0
	// Combined = 32 * 0.6 = 19.2 -> 19
	if *inc.CombinedRisk != 19 {
		t.Errorf("Expected CombinedRisk 19, got %d", *inc.CombinedRisk)
	}
	if *inc.AttackChainState != "NETWORK_ACTIVITY" {
		t.Errorf("Expected NETWORK_ACTIVITY, got %s", *inc.AttackChainState)
	}
	
	// Confidence: Baseline 100 * 0.4 (WEAK) = 40
	if *inc.Confidence != 40 {
		t.Errorf("Expected Confidence 40, got %d", *inc.Confidence)
	}
}

func TestCalculateRisk_DecayBoundary(t *testing.T) {
	tests := []struct {
		name    string
		silence float64
		want    int
	}{
		{"0 hours", 0.0, 79},
		{"6 hours", 6.0, 59},  // 79 * (1 - 6/24) = 79 * 0.75 = 59.25 -> 59
		{"12 hours", 12.0, 40}, // 79 * 0.5 = 39.5 -> 40
		{"18 hours", 18.0, 20}, // 79 * 0.25 = 19.75 -> 20
		{"23.99 hours", 23.99, 0}, // 79 * (1 - 23.99/24) = 0.03 -> 0
		{"24 hours", 24.0, 0},
		{">24 hours", 48.0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			inc := &models.Incident{LastSeen: now.Add(-time.Duration(tt.silence * float64(time.Hour)))}
			findings := []models.FindingRef{
				{FindingID: "f1", RuleID: "process_execution", Severity: "HIGH"},
			}
			
			CalculateRisk(inc, "CRITICAL", "UNKNOWN", now, findings)

			if *inc.DynamicRisk != tt.want {
				t.Errorf("Expected DynamicRisk %d, got %d", tt.want, *inc.DynamicRisk)
			}
			
			// Static risk must never decay
			if *inc.StaticRisk != 80 {
				t.Errorf("Expected StaticRisk 80, got %d", *inc.StaticRisk)
			}
		})
	}
}

func TestCalculateRisk_UnknownBehavior(t *testing.T) {
	inc := &models.Incident{LastSeen: time.Now()}
	findings := []models.FindingRef{
		{FindingID: "f1", RuleID: "generic", Severity: "INFO"},
	}

	CalculateRisk(inc, "UNKNOWN", "UNKNOWN", time.Now(), findings)

	if *inc.StaticRisk != 0 {
		t.Errorf("UNKNOWN CVE should map to StaticRisk 0, got %d", *inc.StaticRisk)
	}
	
	// Confidence: INFO maps to UNKNOWN -> x0.1 -> 10
	if *inc.Confidence != 10 {
		t.Errorf("Expected Confidence 10, got %d", *inc.Confidence)
	}
	
	// Attack chain should be EXPOSURE (generic)
	if *inc.AttackChainState != "EXPOSURE" {
		t.Errorf("Expected EXPOSURE, got %s", *inc.AttackChainState)
	}
}

func TestCalculateRisk_PolicyBlock(t *testing.T) {
	inc := &models.Incident{LastSeen: time.Now()}
	CalculateRisk(inc, "CRITICAL", "BLOCK", time.Now(), []models.FindingRef{})
	
	// 80 (CRITICAL) + 20 (BLOCK) = 100
	if *inc.StaticRisk != 100 {
		t.Errorf("Expected StaticRisk 100 with BLOCK, got %d", *inc.StaticRisk)
	}
}

func TestCalculateRisk_InvalidAttackSequence(t *testing.T) {
	inc := &models.Incident{LastSeen: time.Now()}
	// Only network activity, no execution or exposure
	findings := []models.FindingRef{
		{FindingID: "f1", RuleID: "suspicious_network", Severity: "HIGH"},
	}
	
	CalculateRisk(inc, "INFO", "UNKNOWN", time.Now(), findings)
	
	// Ensure we only advance to NETWORK_ACTIVITY, but do not hallucinate IMPACT
	if *inc.AttackChainState != "NETWORK_ACTIVITY" {
		t.Errorf("Expected NETWORK_ACTIVITY, got %s", *inc.AttackChainState)
	}
}

func TestCalculateRisk_ImpactInference(t *testing.T) {
	inc := &models.Incident{LastSeen: time.Now()}
	findings := []models.FindingRef{
		{FindingID: "f1", RuleID: "process_execution", Severity: "CRITICAL"}, // Execution
	}
	CalculateRisk(inc, "HIGH", "UNKNOWN", time.Now(), findings)
	
	if *inc.AttackChainState == "IMPACT" {
		t.Errorf("Engine hallucinated IMPACT from EXECUTION")
	}
}
