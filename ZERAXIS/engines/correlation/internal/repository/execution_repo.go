package repository

import (
	"context"
	"fmt"
)

// ExecutionRecord represents the joined authoritative state needed for recovery generation.
type ExecutionRecord struct {
	ExecutionID           string
	RecommendationID      string
	IncidentID            string
	IncidentStatus        string
	RecommendationStatus  string
	TargetAssetID         string
	TargetContainerID     string
	TargetImageDigest     string
	ErrorLog              string
	VerificationStatus    string
	RecoveryForExecutionID *string
}

// GetExecutionRecord retrieves the joined execution, recommendation, and incident state.
func (r *Repository) GetExecutionRecord(ctx context.Context, execID string) (*ExecutionRecord, error) {
	query := `
		SELECT 
			er.execution_id,
			er.recommendation_id,
			ir.incident_id,
			inc.status,
		ir.status,
			COALESCE(ir.target_asset_id, ''),
			COALESCE(ir.target_container_id, ''),
			COALESCE(ir.target_image_digest, ''),
			COALESCE(er.error_log, ''),
			COALESCE(er.verification_status, ''),
			ir.recovery_for_execution_id
		FROM execution_results er
		JOIN incident_recommendations ir ON er.recommendation_id = ir.recommendation_id
		JOIN incidents inc ON ir.incident_id = inc.incident_id
		WHERE er.execution_id = $1
	`
	var rec ExecutionRecord
	var tAsset, tContainer, tDigest, errLog, verStatus *string

	err := r.db.QueryRowContext(ctx, query, execID).Scan(
		&rec.ExecutionID,
		&rec.RecommendationID,
		&rec.IncidentID,
		&rec.IncidentStatus,
		&rec.RecommendationStatus,
		&tAsset,
		&tContainer,
		&tDigest,
		&errLog,
		&verStatus,
		&rec.RecoveryForExecutionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get execution record: %w", err)
	}
	
	if tAsset != nil { rec.TargetAssetID = *tAsset }
	if tContainer != nil { rec.TargetContainerID = *tContainer }
	if tDigest != nil { rec.TargetImageDigest = *tDigest }
	if errLog != nil { rec.ErrorLog = *errLog }
	if verStatus != nil { rec.VerificationStatus = *verStatus }
	
	return &rec, nil
}
