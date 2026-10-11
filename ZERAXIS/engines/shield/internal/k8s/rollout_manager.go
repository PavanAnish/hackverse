package k8s

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type RolloutManager struct {
	db     *sql.DB
	client kubernetes.Interface
}

func NewRolloutManager(db *sql.DB, client kubernetes.Interface) *RolloutManager {
	return &RolloutManager{db: db, client: client}
}

func (rm *RolloutManager) ApplyCanaryStage(ctx context.Context, snapshotID string, percentage int) error {
	var regoText, checksum string
	err := rm.db.QueryRowContext(ctx, `SELECT rego_text, checksum FROM k8s_policy_snapshots WHERE snapshot_id = $1`, snapshotID).Scan(&regoText, &checksum)
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %v", err)
	}

	clusters, err := rm.getAllClusters(ctx)
	if err != nil {
		return err
	}

	selected := rm.SelectClustersForPercentage(clusters, percentage)

	for _, cid := range selected {
		if err := rm.pushToCluster(ctx, cid, snapshotID, regoText); err != nil {
			return fmt.Errorf("failed to push to cluster %s: %v", cid, err)
		}
	}

	status := fmt.Sprintf("CANARY_%d", percentage)
	if percentage == 100 {
		status = "GENERAL_AVAILABILITY"
	}
	_, err = rm.db.ExecContext(ctx, `UPDATE k8s_policy_snapshots SET rollout_status = $1 WHERE snapshot_id = $2`, status, snapshotID)
	return err
}

func (rm *RolloutManager) SelectClustersForPercentage(clusters []string, percentage int) []string {
	if percentage >= 100 {
		return clusters
	}
	if percentage <= 0 {
		return []string{}
	}
	// Sort to ensure deterministic rollout selection
	sorted := make([]string, len(clusters))
	copy(sorted, clusters)
	sort.Strings(sorted)

	count := int(math.Ceil(float64(len(sorted)) * float64(percentage) / 100.0))
	if count > len(sorted) {
		count = len(sorted)
	}
	return sorted[:count]
}

func (rm *RolloutManager) VerifyCanaryStage(ctx context.Context, snapshotID string, percentage int) (bool, error) {
	// In a real system, this would query Prometheus or K8s metrics for 500s or webhook errors.
	// Here, we simulate a health check passing.
	return true, nil
}

func (rm *RolloutManager) PromoteToNextStage(ctx context.Context, snapshotID string) error {
	_, err := rm.db.ExecContext(ctx, `UPDATE k8s_policy_snapshots SET rollout_completed_at = NOW() WHERE snapshot_id = $1 AND rollout_status = 'GENERAL_AVAILABILITY'`, snapshotID)
	return err
}

func (rm *RolloutManager) getAllClusters(ctx context.Context) ([]string, error) {
	// For simulation, we pull from registrations or assume fixed list if empty.
	// Since tests might not pre-register webhooks, we'll pull from k8s_cluster_policy_state or registrations.
	// Let's use k8s_webhook_registrations as the source of truth.
	rows, err := rm.db.QueryContext(ctx, `SELECT DISTINCT cluster_id FROM k8s_webhook_registrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		clusters = append(clusters, c)
	}
	return clusters, nil
}

func (rm *RolloutManager) pushToCluster(ctx context.Context, clusterID, snapshotID, regoText string) error {
	// DB Transaction for state immutability
	tx, err := rm.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var current sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT current_snapshot_id FROM k8s_cluster_policy_state WHERE cluster_id = $1 FOR UPDATE`, clusterID).Scan(&current)
	
	if err == sql.ErrNoRows {
		_, err = tx.ExecContext(ctx, `INSERT INTO k8s_cluster_policy_state (state_id, cluster_id, current_snapshot_id, applied_at) VALUES ($1, $2, $3, NOW())`, uuid.New().String(), clusterID, snapshotID)
	} else if err == nil {
		if current.Valid && current.String != snapshotID {
			_, err = tx.ExecContext(ctx, `UPDATE k8s_cluster_policy_state SET previous_snapshot_id = current_snapshot_id, current_snapshot_id = $1, applied_at = NOW() WHERE cluster_id = $2`, snapshotID, clusterID)
		}
	}
	if err != nil {
		return err
	}

	// K8s ConfigMap Push
	immutable := true
	cmName := fmt.Sprintf("shield-policy-%s", clusterID)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: "zeraxis",
			Labels:    map[string]string{"cluster_id": clusterID, "snapshot_id": snapshotID},
		},
		Data: map[string]string{
			"policy.rego": regoText,
		},
		Immutable: &immutable,
	}

	// Fake client or real client interaction
	cmClient := rm.client.CoreV1().ConfigMaps("zeraxis")
	existing, err := cmClient.Get(ctx, cmName, metav1.GetOptions{})
	if err != nil || existing == nil {
		_, err = cmClient.Create(ctx, cm, metav1.CreateOptions{})
	} else {
		// Cannot update immutable configmap directly, we recreate or assume it's updated if we are allowed.
		// In K8s, immutable ConfigMaps require deletion then creation for changes, or we use unique names.
		// For simplicity in this mock, we delete and recreate.
		cmClient.Delete(ctx, cmName, metav1.DeleteOptions{})
		_, err = cmClient.Create(ctx, cm, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("k8s api error: %v", err)
	}

	return tx.Commit()
}
