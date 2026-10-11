package evaluator

import (
	"context"
	"database/sql"

	"github.com/open-policy-agent/opa/rego"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/cache"
)

type OPAEvaluator struct {
	db       *sql.DB
	cache    *cache.MaterializedView
	query    rego.PreparedEvalQuery
	policyID string
}

func NewOPAEvaluator(db *sql.DB, cacheView *cache.MaterializedView) *OPAEvaluator {
	return &OPAEvaluator{
		db:    db,
		cache: cacheView,
	}
}

func (e *OPAEvaluator) LoadPolicy(ctx context.Context, policyID string) error {
	var regoText string
	if policyID == "" || policyID == "ACTIVE" {
		var err error
		regoText, e.policyID, err = LoadActivePolicy(e.db)
		if err != nil {
			return err
		}
	} else {
		err := e.db.QueryRowContext(ctx, "SELECT rego_text FROM policy_definitions WHERE policy_id = $1", policyID).Scan(&regoText)
		if err != nil {
			return err
		}
		e.policyID = policyID
	}

	compiled, err := CompilePolicy(regoText)
	if err != nil {
		return err
	}
	e.query = compiled.(rego.PreparedEvalQuery)
	return nil
}

func (e *OPAEvaluator) Evaluate(ctx context.Context, req AdmissionRequest) (Decision, error) {
	// 1. Read cache first (fast path)
	blockReason, isAllowed, _, err := e.cache.Get(ctx, req.Fingerprint)
	if err != nil {
		// Fail-closed on cache/DB error
		return Decision{Allowed: false, Reason: "CACHE_ERROR: " + err.Error()}, err
	}

	// If cache says BLOCKED: return BLOCKED immediately
	if !isAllowed {
		return Decision{Allowed: false, Reason: blockReason, PolicyVersion: e.policyID}, nil
	}

	// 2. Cache says ALLOWED or miss -> run OPA policy
	input := map[string]interface{}{
		"fingerprint":  req.Fingerprint,
		"image_digest": req.ImageDigest,
		"registry":     req.Registry,
		"labels":       req.Labels,
	}

	results, err := e.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return Decision{Allowed: false, Reason: "EVAL_ERROR: " + err.Error()}, err
	}

	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return Decision{Allowed: false, Reason: "OPA_NO_RESULT"}, nil
	}

	val, ok := results[0].Expressions[0].Value.(map[string]interface{})
	if !ok {
		return Decision{Allowed: false, Reason: "OPA_INVALID_FORMAT"}, nil
	}

	allowed, _ := val["allowed"].(bool)
	reason, _ := val["block_reason"].(string)

	if !allowed && reason == "" {
		reason = "DENIED_BY_POLICY"
	}

	return Decision{
		Allowed:       allowed,
		Reason:        reason,
		PolicyVersion: e.policyID,
	}, nil
}
