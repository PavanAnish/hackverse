package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/models"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/policy"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/repository"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/sbom"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/scanner"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

type Engine struct {
	repo         *repository.Postgres
	scanner      scanner.Scanner
	publisher    events.Publisher
	discoveryURL string
	policyConfig policy.Config
	scanInterval time.Duration
	registryURL  string
}

func NewEngine(scanner scanner.Scanner, repo *repository.Postgres, pub events.Publisher, discoveryURL string, registryURL string, policyConfig policy.Config, scanInterval time.Duration) *Engine {
	return &Engine{
		scanner:      scanner,
		repo:         repo,
		publisher:    pub,
		discoveryURL: discoveryURL,
		registryURL:  registryURL,
		policyConfig: policyConfig,
		scanInterval: scanInterval,
	}
}

func (e *Engine) Run(ctx context.Context) {
	e.RunOnce(ctx)
	ticker := time.NewTicker(e.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Supply chain engine shutting down")
			return
		case <-ticker.C:
			e.RunOnce(ctx)
		}
	}
}

func (e *Engine) RunOnce(ctx context.Context) {
	start := time.Now()
	slog.Info("Starting supply chain cycle")

	cycleCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	assets, err := e.fetchAssets(cycleCtx)
	if err != nil {
		slog.Error("Failed to fetch assets from discovery", "error", err)
		return
	}

	uniqueImages := make(map[string][]models.DiscoveryAsset)
	for _, asset := range assets {
		if asset.Image == "" {
			continue
		}
		scanImage := strings.Replace(asset.Image, "127.0.0.1:5001", e.registryURL, 1)
		scanImage = strings.Replace(scanImage, "localhost:5001", e.registryURL, 1)
		uniqueImages[scanImage] = append(uniqueImages[scanImage], asset)
	}

	for imageRef, mappedAssets := range uniqueImages {
		if !strings.HasPrefix(imageRef, e.registryURL) {
			slog.Debug("Skipping non-registry image", "image", imageRef)
			continue
		}
		e.scanImage(cycleCtx, imageRef, mappedAssets)
	}

	slog.Info("Supply chain cycle complete", "duration_ms", time.Since(start).Milliseconds())
}

func (e *Engine) publishEvent(ctx context.Context, eventType string, imageDigest string, severity string, metadata map[string]interface{}) {
	evt := events.NewEvent(eventType, events.SourceSupplyChain, time.Now())
	evt.ImageDigest = imageDigest
	if severity != "" {
		evt.Severity = severity
	}
	evt.Subject = events.GenerateSubject(events.SourceSupplyChain, eventType)
	metaBytes, _ := json.Marshal(metadata)
	evt.Metadata = metaBytes

	if err := evt.Validate(); err != nil {
		slog.Error("Invalid unified event generated in supply chain", "error", err, "event_type", eventType)
		return
	}

	ueBytes, _ := json.Marshal(evt)
	if err := e.repo.InsertUnifiedEvent(ctx, ueBytes); err != nil {
		slog.Error("Failed to persist supply chain event to PostgreSQL", "error", err, "event_type", eventType)
		return
	}

	if err := e.publisher.Publish(ctx, evt); err != nil {
		slog.Error("Failed to publish supply chain event to NATS", "error", err, "event_type", eventType)
	}
}

func (e *Engine) scanImage(ctx context.Context, imageRef string, assets []models.DiscoveryAsset) {
	slog.Info("Processing image", "image", imageRef, "asset_count", len(assets))

	digest, err := e.getRegistryDigest(ctx, imageRef)
	if err != nil {
		slog.Error("Failed to fetch image digest", "image", imageRef, "error", err)
		return
	}
	
	imageID := digest

	img := models.Image{
		ImageID:       imageID,
		Repository:    imageRef,
		Tag:           "latest",
		Digest:        digest,
		DiscoveredAt:  time.Now(),
		LastScannedAt: time.Now(),
	}

	if err := e.repo.UpsertImage(ctx, img); err != nil {
		slog.Error("Failed to upsert image", "image", imageRef, "error", err)
		return
	}

	for _, asset := range assets {
		if err := e.repo.UpsertAssetImage(ctx, asset.AssetID, imageID); err != nil {
			slog.Error("Failed to map asset to image", "asset", asset.AssetID, "error", err)
		}
	}

	scan := models.Scan{
		ScanID:    uuid.New().String(),
		ImageID:   imageID,
		ScanType:  "full",
		Status:    "running",
		StartedAt: time.Now(),
	}
	e.repo.CreateScan(ctx, scan)
	
	e.publishEvent(ctx, events.EventTypeScanStarted, imageID, "", map[string]interface{}{
		"scan_id": scan.ScanID,
		"image_ref": imageRef,
	})

	slog.Info("Running Syft", "image", imageRef)
	sbomData, err := e.scanner.RunSyft(ctx, imageRef)
	if err != nil {
		slog.Error("Syft failed", "image", imageRef, "error", err)
		e.repo.CompleteScan(ctx, scan.ScanID, "failed", err.Error())
		e.publishEvent(ctx, events.EventTypeScanFailed, imageID, "", map[string]interface{}{
			"scan_id": scan.ScanID,
			"error": err.Error(),
			"stage": "sbom_generation",
		})
		return
	}

	cdx, err := sbom.ParseCycloneDX(sbomData)
	if err != nil {
		slog.Error("Failed to parse CycloneDX", "error", err)
		e.repo.CompleteScan(ctx, scan.ScanID, "failed", "sbom parse error")
		e.publishEvent(ctx, events.EventTypeScanFailed, imageID, "", map[string]interface{}{
			"scan_id": scan.ScanID,
			"error": "sbom parse error",
		})
		return
	}

	docDigest := sha256.Sum256(sbomData)
	sbomModel := models.SBOM{
		SBOMID:         uuid.New().String(),
		ImageID:        imageID,
		ScanID:         scan.ScanID,
		Format:         "cyclonedx",
		GeneratedAt:    time.Now(),
		Tool:           "syft",
		DocumentDigest: hex.EncodeToString(docDigest[:]),
		PackageCount:   len(cdx.Components),
		RawSBOM:        sbomData,
	}
	e.repo.CreateSBOM(ctx, sbomModel)
	
	e.publishEvent(ctx, events.EventTypeSBOMGenerated, imageID, "", map[string]interface{}{
		"scan_id": scan.ScanID,
		"sbom_id": sbomModel.SBOMID,
		"package_count": sbomModel.PackageCount,
	})

	for _, comp := range cdx.Components {
		eco := normalizeEcosystem(comp.PURL, "")
		pkgIDHash := sha256.Sum256([]byte(comp.Name + comp.Version + eco))
		pkgID := hex.EncodeToString(pkgIDHash[:])

		pkg := models.Package{
			PackageID:   pkgID,
			Name:        comp.Name,
			Version:     comp.Version,
			PackageType: eco,
			PURL:        comp.PURL,
		}
		e.repo.UpsertPackage(ctx, pkg)
		e.repo.LinkSBOMPackage(ctx, sbomModel.SBOMID, pkgID)
	}

	slog.Info("Running Trivy", "image", imageRef)
	tmpFile, err := os.CreateTemp("", "sbom-*.json")
	if err != nil {
		slog.Error("Failed to create temp file for SBOM", "error", err)
		e.repo.CompleteScan(ctx, scan.ScanID, "failed", err.Error())
		e.publishEvent(ctx, events.EventTypeScanFailed, imageID, "", map[string]interface{}{
			"scan_id": scan.ScanID,
			"error": err.Error(),
			"stage": "vulnerability_scan",
		})
		return
	}
	defer os.Remove(tmpFile.Name())
	
	if _, err := tmpFile.Write(sbomData); err != nil {
		slog.Error("Failed to write temp SBOM", "error", err)
		e.repo.CompleteScan(ctx, scan.ScanID, "failed", err.Error())
		e.publishEvent(ctx, events.EventTypeScanFailed, imageID, "", map[string]interface{}{
			"scan_id": scan.ScanID,
			"error": err.Error(),
			"stage": "vulnerability_scan",
		})
		return
	}
	tmpFile.Close()

	trivyData, err := e.scanner.RunTrivy(ctx, tmpFile.Name())
	if err != nil {
		slog.Error("Trivy failed", "image", imageRef, "error", err)
		e.repo.CompleteScan(ctx, scan.ScanID, "failed", err.Error())
		e.publishEvent(ctx, events.EventTypeScanFailed, imageID, "", map[string]interface{}{
			"scan_id": scan.ScanID,
			"error": err.Error(),
			"stage": "vulnerability_scan",
		})
		return
	}

	report, err := sbom.ParseTrivy(trivyData)
	if err != nil {
		slog.Error("Failed to parse Trivy", "error", err)
		e.repo.CompleteScan(ctx, scan.ScanID, "failed", "trivy parse error")
		e.publishEvent(ctx, events.EventTypeScanFailed, imageID, "", map[string]interface{}{
			"scan_id": scan.ScanID,
			"error": "trivy parse error",
		})
		return
	}

	var crit, high, med, low int
	for _, res := range report.Results {
		eco := normalizeEcosystem("", res.Class)
		for _, v := range res.Vulnerabilities {
			pkgIDHash := sha256.Sum256([]byte(v.PkgName + v.InstalledVersion + eco))
			pkgID := hex.EncodeToString(pkgIDHash[:])

			e.repo.UpsertPackage(ctx, models.Package{
				PackageID:   pkgID,
				Name:        v.PkgName,
				Version:     v.InstalledVersion,
				PackageType: eco,
			})

			vulnIDHash := sha256.Sum256([]byte(v.VulnerabilityID + pkgID + imageID))
			vulnModel := models.Vulnerability{
				VulnID:                  hex.EncodeToString(vulnIDHash[:]),
				VulnerabilityIdentifier: v.VulnerabilityID,
				PackageID:               pkgID,
				ImageID:                 imageID,
				ScanID:                  scan.ScanID,
				Severity:                v.Severity,
				InstalledVersion:        v.InstalledVersion,
				FixedVersion:            v.FixedVersion,
				Title:                   v.Title,
				Description:             v.Description,
				Source:                  "trivy",
				DetectedAt:              time.Now(),
				Status:                  "active",
			}
			e.repo.UpsertVulnerability(ctx, vulnModel)

			e.publishEvent(ctx, events.EventTypeVulnerabilityFound, imageID, v.Severity, map[string]interface{}{
				"scan_id": scan.ScanID,
				"vuln_id": vulnModel.VulnID,
				"identifier": v.VulnerabilityID,
				"package_name": v.PkgName,
				"installed_version": v.InstalledVersion,
				"fixed_version": v.FixedVersion,
			})

			switch strings.ToUpper(v.Severity) {
			case "CRITICAL":
				crit++
			case "HIGH":
				high++
			case "MEDIUM":
				med++
			case "LOW":
				low++
			}
		}
	}

	action, reason := e.policyConfig.Evaluate(crit, high, med, low)
	decision := models.PolicyDecision{
		DecisionID:    uuid.New().String(),
		ImageID:       imageID,
		ScanID:        scan.ScanID,
		PolicyVersion: "1.0",
		Result:        string(action),
		Reason:        reason,
		CriticalCount: crit,
		HighCount:     high,
		MediumCount:   med,
		LowCount:      low,
		EvaluatedAt:   time.Now(),
	}
	if err := e.repo.CreatePolicyDecision(ctx, decision); err != nil {
		slog.Error("Failed to store policy decision", "error", err)
	}

	e.publishEvent(ctx, events.EventTypePolicyDecisionCreated, imageID, "", map[string]interface{}{
		"scan_id": scan.ScanID,
		"decision_id": decision.DecisionID,
		"result": decision.Result,
		"reason": decision.Reason,
	})

	if err := e.repo.CompleteScan(ctx, scan.ScanID, "completed", ""); err != nil {
		slog.Error("Failed to complete scan", "error", err)
	}

	e.publishEvent(ctx, events.EventTypeScanCompleted, imageID, "", map[string]interface{}{
		"scan_id": scan.ScanID,
		"critical": crit,
		"high": high,
		"medium": med,
		"low": low,
		"decision": string(action),
	})

	slog.Info("Image scan completed successfully", "image", imageRef, "vulnerabilities", crit+high+med+low, "decision", action)
}

func normalizeEcosystem(syftPURL, trivyType string) string {
	if trivyType != "" {
		trivyType = strings.ToLower(trivyType)
		// Map some common trivy types to standard names
		if trivyType == "gomod" {
			return "golang"
		} else if trivyType == "alpine" {
			return "apk"
		}
		return trivyType
	}
	
	if syftPURL != "" {
		// e.g. pkg:golang/github.com/... -> golang
		parts := strings.Split(syftPURL, "/")
		if len(parts) > 0 {
			scheme := strings.Split(parts[0], ":")
			if len(scheme) == 2 {
				return scheme[1]
			}
		}
	}
	return "unknown"
}

func (e *Engine) getRegistryDigest(ctx context.Context, imageRef string) (string, error) {
	// imageRef example: registry:5000/zeraxis/gateway:latest
	parts := strings.Split(imageRef, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid image format: %s", imageRef)
	}
	registry := parts[0]
	rest := strings.Join(parts[1:], "/")
	
	nameAndTag := strings.Split(rest, ":")
	name := nameAndTag[0]
	tag := "latest"
	if len(nameAndTag) > 1 {
		tag = nameAndTag[1]
	}

	url := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, name, tag)
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry API returned %d", resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("no digest header returned")
	}

	return digest, nil
}

func (e *Engine) fetchAssets(ctx context.Context) ([]models.DiscoveryAsset, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", e.discoveryURL+"/api/v1/assets", nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("discovery API returned %d", resp.StatusCode)
	}

	var assets []models.DiscoveryAsset
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return nil, err
	}
	return assets, nil
}
