package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
)

// UpsertRecommendation inserts or updates a recommendation based on its unique deterministic fingerprint.
// Returns (recommendation, isNew, error).
func (r *Repository) UpsertRecommendation(ctx context.Context, rec *models.Recommendation, fingerprint string) (*models.Recommendation, bool, error) {
	query := `
		INSERT INTO incident_recommendations (
			recommendation_id, incident_id, fingerprint, action, target_type, target_id, target_asset_id, target_container_id, target_image_digest, recovery_for_execution_id,
			priority, confidence, reason, evidence, model_version, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
		ON CONFLICT (fingerprint) DO UPDATE SET
			priority = EXCLUDED.priority,
			confidence = EXCLUDED.confidence,
			reason = EXCLUDED.reason,
			evidence = EXCLUDED.evidence,
			status = CASE 
						-- preserve ACKNOWLEDGED/DISMISSED/APPROVED states if manually altered, otherwise stay PROPOSED
						WHEN incident_recommendations.status != 'PROPOSED' THEN incident_recommendations.status 
						ELSE EXCLUDED.status 
					 END,
			updated_at = NOW()
		RETURNING recommendation_id, incident_id, action, target_type, target_id, target_asset_id, target_container_id, target_image_digest, recovery_for_execution_id, priority, confidence, reason, evidence, model_version, status, created_at, updated_at, (xmax = 0) AS is_insert
	`

	var out models.Recommendation
	var isInsert bool
	var ev []byte
	err := r.db.QueryRowContext(ctx, query,
		rec.RecommendationID, rec.IncidentID, fingerprint, rec.Action,
		rec.TargetType, rec.TargetID, rec.TargetAssetID, rec.TargetContainerID, rec.TargetImageDigest, rec.RecoveryForExecutionID, rec.Priority, rec.Confidence,
		rec.Reason, rec.Evidence, rec.ModelVersion, rec.Status,
		rec.CreatedAt, rec.UpdatedAt,
	).Scan(
		&out.RecommendationID, &out.IncidentID, &out.Action,
		&out.TargetType, &out.TargetID, &out.TargetAssetID, &out.TargetContainerID, &out.TargetImageDigest, &out.RecoveryForExecutionID, &out.Priority, &out.Confidence,
		&out.Reason, &ev, &out.ModelVersion, &out.Status,
		&out.CreatedAt, &out.UpdatedAt, &isInsert,
	)

	if err != nil {
		return nil, false, err
	}
	out.Evidence = json.RawMessage(ev)

	return &out, isInsert, nil
}

// InsertRecoveryRecommendation performs an atomic INSERT ... ON CONFLICT DO NOTHING.
// Returns isNew=true if inserted, isNew=false if the fingerprint already exists.
func (r *Repository) InsertRecoveryRecommendation(ctx context.Context, rec *models.Recommendation, fingerprint string) (bool, error) {
	query := `
		INSERT INTO incident_recommendations (
			recommendation_id, incident_id, fingerprint, action, target_type, target_id, target_asset_id, target_container_id, target_image_digest, recovery_for_execution_id,
			priority, confidence, reason, evidence, model_version, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
		ON CONFLICT (fingerprint) DO NOTHING
	`

	res, err := r.db.ExecContext(ctx, query,
		rec.RecommendationID, rec.IncidentID, fingerprint, rec.Action,
		rec.TargetType, rec.TargetID, rec.TargetAssetID, rec.TargetContainerID, rec.TargetImageDigest, rec.RecoveryForExecutionID, rec.Priority, rec.Confidence,
		rec.Reason, rec.Evidence, rec.ModelVersion, rec.Status,
		rec.CreatedAt, rec.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	rowsAffected, _ := res.RowsAffected()
	return rowsAffected > 0, nil
}

// GetIncidentRecommendations returns all recommendations for an incident.
func (r *Repository) GetIncidentRecommendations(ctx context.Context, incidentID string) ([]models.Recommendation, error) {
	query := `
		SELECT recommendation_id, incident_id, action, target_type, target_id, target_asset_id, target_container_id, target_image_digest, recovery_for_execution_id, priority, confidence, reason, evidence, model_version, status, created_at, updated_at
		FROM incident_recommendations
		WHERE incident_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, incidentID)
	if err != nil {
		return nil, fmt.Errorf("query incident recommendations: %w", err)
	}
	defer rows.Close()

	var results []models.Recommendation
	for rows.Next() {
		var rec models.Recommendation
		var ev []byte
		if err := rows.Scan(
			&rec.RecommendationID, &rec.IncidentID, &rec.Action,
			&rec.TargetType, &rec.TargetID, &rec.TargetAssetID, &rec.TargetContainerID, &rec.TargetImageDigest, &rec.RecoveryForExecutionID, &rec.Priority, &rec.Confidence,
			&rec.Reason, &ev, &rec.ModelVersion, &rec.Status,
			&rec.CreatedAt, &rec.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan incident recommendation: %w", err)
		}
		rec.Evidence = json.RawMessage(ev)
		results = append(results, rec)
	}
	return results, rows.Err()
}

// GetRecommendationByID returns a specific recommendation by its ID.
func (r *Repository) GetRecommendationByID(ctx context.Context, recID string) (*models.Recommendation, error) {
	query := `
		SELECT recommendation_id, incident_id, action, target_type, target_id, target_asset_id, target_container_id, target_image_digest, recovery_for_execution_id, priority, confidence, reason, evidence, model_version, status, created_at, updated_at
		FROM incident_recommendations
		WHERE recommendation_id = $1
	`
	var rec models.Recommendation
	var ev []byte
	err := r.db.QueryRowContext(ctx, query, recID).Scan(
		&rec.RecommendationID, &rec.IncidentID, &rec.Action,
		&rec.TargetType, &rec.TargetID, &rec.TargetAssetID, &rec.TargetContainerID, &rec.TargetImageDigest, &rec.RecoveryForExecutionID, &rec.Priority, &rec.Confidence,
		&rec.Reason, &ev, &rec.ModelVersion, &rec.Status,
		&rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan recommendation by id: %w", err)
	}
	rec.Evidence = json.RawMessage(ev)
	return &rec, nil
}
