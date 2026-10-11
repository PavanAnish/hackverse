package repository

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
)

// RiskState stores Phase 3 properties required for static risk.
type RiskState struct {
	MaxCVESeverity string
	PolicyResult   string
}

// GetSupplyChainRiskState retrieves the supply chain context for an image digest.
func (r *Repository) GetSupplyChainRiskState(ctx context.Context, imageDigest string) (RiskState, error) {
	if imageDigest == "" {
		return RiskState{MaxCVESeverity: "UNKNOWN", PolicyResult: "UNKNOWN"}, nil
	}

	state := RiskState{MaxCVESeverity: "UNKNOWN", PolicyResult: "UNKNOWN"}

	// 1. Get max vulnerability severity.
	// Map to rank and select highest.
	var severity string
	err := r.db.QueryRowContext(ctx, `
		SELECT severity FROM supply_chain_vulnerabilities 
		WHERE image_id = $1 
		ORDER BY 
			CASE severity
				WHEN 'CRITICAL' THEN 4
				WHEN 'HIGH' THEN 3
				WHEN 'MEDIUM' THEN 2
				WHEN 'LOW' THEN 1
				ELSE 0
			END DESC
		LIMIT 1
	`, imageDigest).Scan(&severity)

	if err != nil && err != sql.ErrNoRows {
		return state, fmt.Errorf("query vulnerabilities: %w", err)
	}
	if err == nil {
		state.MaxCVESeverity = severity
	}

	// 2. Get latest policy decision.
	var policyResult string
	err = r.db.QueryRowContext(ctx, `
		SELECT result FROM supply_chain_policy_decisions 
		WHERE image_id = $1 
		ORDER BY evaluated_at DESC 
		LIMIT 1
	`, imageDigest).Scan(&policyResult)

	if err != nil && err != sql.ErrNoRows {
		return state, fmt.Errorf("query policies: %w", err)
	}
	if err == nil {
		state.PolicyResult = policyResult
	}

	return state, nil
}

// GetIncidentFindingRefs fetches the full finding context needed for Phase 6.3 Risk Calculation.
func (r *Repository) GetIncidentFindingRefs(ctx context.Context, incidentID string) ([]models.FindingRef, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT f.finding_id, f.status, f.rule_id, f.severity, 
		       coalesce(f.asset_id,''), coalesce(f.image_digest,'')
		FROM incident_findings inf
		JOIN findings f ON inf.finding_id = f.finding_id
		WHERE inf.incident_id = $1
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("query incident findings: %w", err)
	}
	defer rows.Close()

	var refs []models.FindingRef
	for rows.Next() {
		var ref models.FindingRef
		if err := rows.Scan(
			&ref.FindingID, &ref.Status, &ref.RuleID, 
			&ref.Severity, &ref.AssetID, &ref.ImageDigest,
		); err != nil {
			return nil, fmt.Errorf("scan finding ref: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// UpdateIncidentRisk updates the Phase 6.3 fields on the incident in the database.
func (r *Repository) UpdateIncidentRisk(ctx context.Context, inc *models.Incident) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE incidents SET
			static_risk = $1,
			dynamic_risk = $2,
			combined_risk = $3,
			confidence = $4,
			risk_model_version = $5,
			risk_level = $6,
			calculated_at = $7,
			attack_chain_state = $8,
			attack_chain_evidence = $9,
			updated_at = NOW()
		WHERE incident_id = $10
	`, 
		inc.StaticRisk, 
		inc.DynamicRisk, 
		inc.CombinedRisk, 
		inc.Confidence, 
		inc.RiskModelVersion, 
		inc.RiskLevel, 
		inc.CalculatedAt, 
		inc.AttackChainState, 
		inc.AttackChainEvidence, 
		inc.IncidentID,
	)
	if err != nil {
		return fmt.Errorf("update incident risk: %w", err)
	}
	return nil
}
