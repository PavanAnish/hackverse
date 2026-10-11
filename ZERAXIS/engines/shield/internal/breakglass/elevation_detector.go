package breakglass

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

type ElevationDetector struct {
	db *sql.DB
	nc *nats.Conn
}

func NewElevationDetector() *ElevationDetector {
	return &ElevationDetector{}
}

func (e *ElevationDetector) Start(db *sql.DB, nc *nats.Conn) error {
	e.db = db
	e.nc = nc
	return nil
}

func (e *ElevationDetector) checkElevationThreshold(ctx context.Context, userID string) (int, bool, bool, error) {
	var count int
	err := e.db.QueryRowContext(ctx, 
		`SELECT COUNT(*) FROM override_requests 
		 WHERE requested_by = $1 AND created_at >= NOW() - INTERVAL '1 hour'`, userID).Scan(&count)
	if err != nil {
		return 0, false, false, err
	}
	return count, count > 0, count >= 3, nil
}

func (e *ElevationDetector) createPolicyAbuseIncident(ctx context.Context, userID string, overrideCount int) error {
	b, _ := json.Marshal(map[string]interface{}{"override_count": overrideCount})
	_, err := e.db.ExecContext(ctx, 
		`INSERT INTO policy_audit_trail (entry_id, event_type, actor_id, cluster_id, details) 
		 VALUES ($1, $2, $3, $4, $5)`,
		uuid.New().String(), "ELEVATION_TRIGGERED", userID, "SYSTEM", b)
	if err != nil {
		return err
	}

	if e.nc != nil {
		event := events.PolicyAbuseIncidentEvent{
			UserID:        userID,
			OverrideCount: overrideCount,
			TimeWindow:    "1h",
			Timestamp:     time.Now(),
		}
		payload, _ := json.Marshal(event)
		return e.nc.Publish("zeraxis.events.correlation.incident.policy_abuse", payload)
	}
	return nil
}

func (e *ElevationDetector) blockUserFromFurtherOverrides(ctx context.Context, userID string) (bool, error) {
	res, err := e.db.ExecContext(ctx, 
		`INSERT INTO shield_blocked_users (user_id, reason) VALUES ($1, $2) ON CONFLICT DO NOTHING`, 
		userID, "POLICY_ABUSE")
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (e *ElevationDetector) CheckAndElevate(ctx context.Context, userID string) error {
	count, _, shouldElevate, err := e.checkElevationThreshold(ctx, userID)
	if err != nil {
		return err
	}
	
	if shouldElevate {
		inserted, err := e.blockUserFromFurtherOverrides(ctx, userID)
		if err != nil {
			return err
		}
		if inserted {
			return e.createPolicyAbuseIncident(ctx, userID, count)
		}
	}
	
	return nil
}
