package sbom

import (
	"encoding/json"
	"fmt"
)

// CycloneDX minimal structures to extract packages
type CycloneDX struct {
	BomFormat   string `json:"bomFormat"`
	SpecVersion string `json:"specVersion"`
	Metadata    struct {
		Timestamp string `json:"timestamp"`
		Component struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"component"`
	} `json:"metadata"`
	Components []Component `json:"components"`
}

type Component struct {
	BOMRef   string `json:"bom-ref"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	PURL     string `json:"purl"`
	Licenses []struct {
		License struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"license"`
	} `json:"licenses"`
}

// TrivyReport minimal structures for extracting vulnerabilities
type TrivyReport struct {
	SchemaVersion int           `json:"SchemaVersion"`
	ArtifactName  string        `json:"ArtifactName"`
	ArtifactType  string        `json:"ArtifactType"`
	Results       []TrivyResult `json:"Results"`
}

type TrivyResult struct {
	Target          string               `json:"Target"`
	Class           string               `json:"Class"`
	Type            string               `json:"Type"`
	Vulnerabilities []TrivyVulnerability `json:"Vulnerabilities"`
}

type TrivyVulnerability struct {
	VulnerabilityID  string `json:"VulnerabilityID"`
	PkgName          string `json:"PkgName"`
	PkgID            string `json:"PkgID"`
	InstalledVersion string `json:"InstalledVersion"`
	FixedVersion     string `json:"FixedVersion"`
	Status           string `json:"Status"`
	Severity         string `json:"Severity"`
	Title            string `json:"Title"`
	Description      string `json:"Description"`
}

func ParseCycloneDX(data []byte) (*CycloneDX, error) {
	var cdx CycloneDX
	if err := json.Unmarshal(data, &cdx); err != nil {
		return nil, fmt.Errorf("failed to parse CycloneDX JSON: %w", err)
	}
	return &cdx, nil
}

func ParseTrivy(data []byte) (*TrivyReport, error) {
	var report TrivyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to parse Trivy JSON: %w", err)
	}
	return &report, nil
}
