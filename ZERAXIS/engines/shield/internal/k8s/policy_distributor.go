package k8s

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type PolicyDistributor struct {
	db      *sql.DB
	manager *RolloutManager
}

func NewPolicyDistributor(db *sql.DB, manager *RolloutManager) *PolicyDistributor {
	return &PolicyDistributor{db: db, manager: manager}
}

func (pd *PolicyDistributor) PushPolicy(ctx context.Context, snapshotID string, policyVersion int) error {
	// Transition 10% -> 50% -> 100%
	return pd.CanaryRollout(ctx, snapshotID, []int{10, 50, 100})
}

func (pd *PolicyDistributor) CanaryRollout(ctx context.Context, snapshotID string, stages []int) error {
	for _, pct := range stages {
		if err := pd.manager.ApplyCanaryStage(ctx, snapshotID, pct); err != nil {
			return fmt.Errorf("failed to apply stage %d%%: %v", pct, err)
		}
		
		healthy, err := pd.manager.VerifyCanaryStage(ctx, snapshotID, pct)
		if err != nil || !healthy {
			return fmt.Errorf("verification failed for stage %d%%: %v", pct, err)
		}

		pd.auditLog(ctx, "CANARY_STAGE_COMPLETE", map[string]interface{}{
			"snapshot_id": snapshotID,
			"stage":       pct,
		})
	}
	
	if err := pd.manager.PromoteToNextStage(ctx, snapshotID); err != nil {
		return err
	}
	
	pd.auditLog(ctx, "ROLLOUT_COMPLETED", map[string]interface{}{"snapshot_id": snapshotID})
	return nil
}

func (pd *PolicyDistributor) RollbackPolicy(ctx context.Context, snapshotID, previousSnapshotID string) error {
	// A rollback acts as a 100% fast-track rollout of the previous snapshot
	err := pd.manager.ApplyCanaryStage(ctx, previousSnapshotID, 100)
	if err != nil {
		return err
	}

	_, err = pd.db.ExecContext(ctx, `UPDATE k8s_policy_snapshots SET rollout_status = 'ROLLED_BACK' WHERE snapshot_id = $1`, snapshotID)
	if err != nil {
		return err
	}

	pd.auditLog(ctx, "POLICY_ROLLED_BACK", map[string]interface{}{
		"bad_snapshot_id":      snapshotID,
		"restored_snapshot_id": previousSnapshotID,
	})
	
	return nil
}

func (pd *PolicyDistributor) GetPolicyState(ctx context.Context, clusterID string) (string, string, error) {
	var current, prev sql.NullString
	err := pd.db.QueryRowContext(ctx, `SELECT current_snapshot_id, previous_snapshot_id FROM k8s_cluster_policy_state WHERE cluster_id = $1`, clusterID).Scan(&current, &prev)
	if err != nil {
		return "", "", err
	}
	return current.String, prev.String, nil
}

func (pd *PolicyDistributor) auditLog(ctx context.Context, eventType string, details map[string]interface{}) {
	b, _ := json.Marshal(details)
	pd.db.ExecContext(ctx, 
		`INSERT INTO policy_audit_trail (entry_id, event_type, actor_id, cluster_id, details) VALUES ($1, $2, $3, $4, $5)`,
		uuid.New().String(), eventType, "SYSTEM", "ALL", b)
}
