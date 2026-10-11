package models

import (
	"encoding/json"
	"time"
)

type Image struct {
	ImageID       string    `json:"image_id"` // Digest
	Repository    string    `json:"repository"`
	Tag           string    `json:"tag"`
	Digest        string    `json:"digest"`
	CreatedAt     time.Time `json:"created_at"`
	SizeBytes     int64     `json:"size_bytes"`
	DiscoveredAt  time.Time `json:"discovered_at"`
	LastScannedAt time.Time `json:"last_scanned_at"`
}

type Scan struct {
	ScanID       string    `json:"scan_id"`
	ImageID      string    `json:"image_id"`
	ScanType     string    `json:"scan_type"`
	Status       string    `json:"status"` // pending, running, completed, failed
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
	Error        string    `json:"error"`
	SyftVersion  string    `json:"syft_version"`
	TrivyVersion string    `json:"trivy_version"`
}

type SBOM struct {
	SBOMID         string          `json:"sbom_id"`
	ImageID        string          `json:"image_id"`
	ScanID         string          `json:"scan_id"`
	Format         string          `json:"format"`
	SpecVersion    string          `json:"spec_version"`
	GeneratedAt    time.Time       `json:"generated_at"`
	Tool           string          `json:"tool"`
	ToolVersion    string          `json:"tool_version"`
	DocumentDigest string          `json:"document_digest"`
	PackageCount   int             `json:"package_count"`
	RawSBOM        json.RawMessage `json:"-"`
}

type Package struct {
	PackageID   string   `json:"package_id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	PackageType string   `json:"package_type"`
	PURL        string   `json:"purl"`
	Licenses    []string `json:"licenses"`
}

type Vulnerability struct {
	VulnID                  string    `json:"vuln_id"`
	VulnerabilityIdentifier string    `json:"vulnerability_identifier"` // e.g. CVE-2023-1234
	PackageID               string    `json:"package_id"`
	ImageID                 string    `json:"image_id"`
	ScanID                  string    `json:"scan_id"`
	Severity                string    `json:"severity"` // CRITICAL, HIGH, MEDIUM, LOW, UNKNOWN
	InstalledVersion        string    `json:"installed_version"`
	FixedVersion            string    `json:"fixed_version"`
	Title                   string    `json:"title"`
	Description             string    `json:"description"`
	Source                  string    `json:"source"`
	DetectedAt              time.Time `json:"detected_at"`
	Status                  string    `json:"status"` // active, fixed, ignored
}

type PolicyDecision struct {
	DecisionID    string    `json:"decision_id"`
	ImageID       string    `json:"image_id"`
	ScanID        string    `json:"scan_id"`
	PolicyVersion string    `json:"policy_version"`
	Result        string    `json:"result"` // BLOCK, WARN, ALLOW
	Reason        string    `json:"reason"`
	CriticalCount int       `json:"critical_count"`
	HighCount     int       `json:"high_count"`
	MediumCount   int       `json:"medium_count"`
	LowCount      int       `json:"low_count"`
	EvaluatedAt   time.Time `json:"evaluated_at"`
}

type AssetSupplyChain struct {
	AssetID        string           `json:"asset_id"`
	Image          Image            `json:"image"`
	SBOMs          []SBOM           `json:"sboms"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities"`
	Decisions      []PolicyDecision `json:"decisions"`
}

// Discovery API Types (to decode from Phase 2 API)
type DiscoveryAsset struct {
	AssetID string `json:"asset_id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	ImageID string `json:"image_id"` // Phase 2 might give the local digest
}
