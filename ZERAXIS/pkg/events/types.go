package events

import (
	"fmt"
	"strings"
)

// Event Types Implemented in Phase 4
const (
	// Discovery Events
	EventTypeAssetDiscovered            = "ASSET_DISCOVERED"
	EventTypeAssetUpdated               = "ASSET_UPDATED"
	EventTypeAssetRemoved               = "ASSET_REMOVED"
	EventTypeNetworkRelationshipCreated = "NETWORK_RELATIONSHIP_CREATED"
	EventTypeNetworkRelationshipRemoved = "NETWORK_RELATIONSHIP_REMOVED"

	// Supply Chain Events
	EventTypeImageDiscovered         = "IMAGE_DISCOVERED"
	EventTypeScanStarted             = "SCAN_STARTED"
	EventTypeScanCompleted           = "SCAN_COMPLETED"
	EventTypeScanFailed              = "SCAN_FAILED"
	EventTypeSBOMGenerated           = "SBOM_GENERATED"
	EventTypeVulnerabilityFound      = "VULNERABILITY_FOUND"
	EventTypePolicyDecisionCreated   = "POLICY_DECISION_CREATED"
)

// Event Types Reserved for Future Phases
const (
	EventTypeVulnerabilityResolved = "VULNERABILITY_RESOLVED"
	EventTypeServiceHealthChanged  = "SERVICE_HEALTH_CHANGED"
	EventTypeProcessExecution      = "PROCESS_EXECUTION"
	EventTypeNetworkConnection     = "NETWORK_CONNECTION"
	EventTypeFileActivity          = "FILE_ACTIVITY"
	EventTypeIncidentCreated       = "INCIDENT_CREATED"
	EventTypeResponseExecuted      = "RESPONSE_EXECUTED"
	EventTypeRecoveryCompleted     = "RECOVERY_COMPLETED"
)

// Source identities
const (
	SourceDiscovery   = "discovery"
	SourceSupplyChain = "supply-chain"
	SourceRuntime     = "runtime"
	SourceCorrelation = "correlation"
	SourceRisk        = "risk"
	SourceResponse    = "response"
)

// Severities
const (
	SeverityInfo     = "INFO"
	SeverityLow      = "LOW"
	SeverityMedium   = "MEDIUM"
	SeverityHigh     = "HIGH"
	SeverityCritical = "CRITICAL"
)

// GenerateSubject deterministically generates a NATS subject from the event type and source.
// e.g. ASSET_DISCOVERED -> zeraxis.events.discovery.asset.discovered
func GenerateSubject(source, eventType string) string {
	domain := strings.ToLower(source)
	domain = strings.ReplaceAll(domain, "-", "") // e.g. supply-chain -> supplychain
	
	action := strings.ToLower(eventType)
	action = strings.ReplaceAll(action, "_", ".") // e.g. ASSET_DISCOVERED -> asset.discovered

	return fmt.Sprintf("zeraxis.events.%s.%s", domain, action)
}

// Phase 6.4 Event Types
const (
	EventTypeRecommendationCreated = "RECOMMENDATION_CREATED"
	EventTypeRecommendationUpdated = "RECOMMENDATION_UPDATED"
)
