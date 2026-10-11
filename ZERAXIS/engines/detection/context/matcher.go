package context

import (
	"strings"
	"path/filepath"
)

type EvidenceStrength string

const (
	StrengthStrong  EvidenceStrength = "STRONG"
	StrengthMedium  EvidenceStrength = "MEDIUM"
	StrengthWeak    EvidenceStrength = "WEAK"
	StrengthUnknown EvidenceStrength = "UNKNOWN"
)

type MatchResult struct {
	Strength        EvidenceStrength
	Vulnerabilities []Vulnerability
	Explanation     string
}

// MatchProcessToVulnerabilities evaluates the mapping between a runtime executable and known vulnerabilities in the image.
func (r *Resolver) MatchProcessToVulnerabilities(ctx ImageContext, executable string) MatchResult {
	if ctx.Unknown {
		return MatchResult{
			Strength:    StrengthUnknown,
			Explanation: "Supply chain context is UNKNOWN (missing scan, missing image, or API failure).",
		}
	}
	if len(ctx.Vulnerabilities) == 0 {
		return MatchResult{
			Strength:    StrengthUnknown,
			Explanation: "No known vulnerabilities affect this image.",
		}
	}

	execName := filepath.Base(executable)
	var strongMatches []Vulnerability
	var mediumMatches []Vulnerability

	for _, pkg := range ctx.Packages {
		isStrong := false
		isMedium := false

		// 1. STRONG MATCH: Exact path match in SBOM
		for _, path := range pkg.Paths {
			if path == executable {
				isStrong = true
				break
			}
		}

		// 2. MEDIUM MATCH: Substring/name match (e.g. process name matches package name)
		if !isStrong {
			if strings.EqualFold(pkg.Name, execName) || strings.Contains(strings.ToLower(pkg.Name), strings.ToLower(execName)) {
				isMedium = true
			}
		}

		if isStrong || isMedium {
			// Find vulnerabilities for this package
			for _, v := range ctx.Vulnerabilities {
				if v.PackageName == pkg.Name {
					if isStrong {
						strongMatches = append(strongMatches, v)
					} else {
						mediumMatches = append(mediumMatches, v)
					}
				}
			}
		}
	}

	if len(strongMatches) > 0 {
		return MatchResult{
			Strength:        StrengthStrong,
			Vulnerabilities: strongMatches,
			Explanation:     "The runtime executable perfectly matches the exact installation path of a vulnerable package recorded in the SBOM.",
		}
	}

	if len(mediumMatches) > 0 {
		return MatchResult{
			Strength:        StrengthMedium,
			Vulnerabilities: mediumMatches,
			Explanation:     "The runtime executable name maps probabilistically to a known vulnerable package, but exact path cryptographic proof is missing.",
		}
	}

	// WEAK MATCH: We have a critical vulnerability in the image, but the process executing isn't obviously related.
	var criticalVulns []Vulnerability
	for _, v := range ctx.Vulnerabilities {
		if v.Severity == "CRITICAL" {
			criticalVulns = append(criticalVulns, v)
		}
	}
	if len(criticalVulns) > 0 {
		return MatchResult{
			Strength:        StrengthWeak,
			Vulnerabilities: criticalVulns,
			Explanation:     "The image contains CRITICAL vulnerabilities, but the executed process cannot be reliably mapped to the affected packages.",
		}
	}

	return MatchResult{
		Strength:    StrengthUnknown,
		Explanation: "Process could not be mapped to any vulnerable package.",
	}
}
