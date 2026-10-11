package context

import (
	"testing"
)

func TestMatchProcessToVulnerabilities_Unknown(t *testing.T) {
	r := &Resolver{}
	ctx := ImageContext{Unknown: true}
	res := r.MatchProcessToVulnerabilities(ctx, "/bin/bash")
	if res.Strength != StrengthUnknown {
		t.Errorf("Expected UNKNOWN, got %v", res.Strength)
	}
}

func TestMatchProcessToVulnerabilities_StrongMatch(t *testing.T) {
	r := &Resolver{}
	ctx := ImageContext{
		Unknown: false,
		Packages: []Package{
			{Name: "bash", Paths: []string{"/bin/bash", "/usr/bin/bash"}},
		},
		Vulnerabilities: []Vulnerability{
			{Identifier: "CVE-2023-1234", Severity: "CRITICAL", PackageName: "bash"},
		},
	}
	res := r.MatchProcessToVulnerabilities(ctx, "/bin/bash")
	if res.Strength != StrengthStrong {
		t.Errorf("Expected STRONG, got %v", res.Strength)
	}
}

func TestMatchProcessToVulnerabilities_MediumMatch(t *testing.T) {
	r := &Resolver{}
	ctx := ImageContext{
		Unknown: false,
		Packages: []Package{
			{Name: "bash", Paths: []string{"/different/path/bash"}}, // Paths don't match exactly
		},
		Vulnerabilities: []Vulnerability{
			{Identifier: "CVE-2023-1234", Severity: "CRITICAL", PackageName: "bash"},
		},
	}
	// Name match but not path match -> MEDIUM
	res := r.MatchProcessToVulnerabilities(ctx, "/bin/bash")
	if res.Strength != StrengthMedium {
		t.Errorf("Expected MEDIUM, got %v", res.Strength)
	}
}

func TestMatchProcessToVulnerabilities_WeakMatch(t *testing.T) {
	r := &Resolver{}
	ctx := ImageContext{
		Unknown: false,
		Packages: []Package{
			{Name: "openssl", Paths: []string{"/usr/bin/openssl"}},
		},
		Vulnerabilities: []Vulnerability{
			{Identifier: "CVE-2023-9999", Severity: "CRITICAL", PackageName: "openssl"},
		},
	}
	// An unrelated executable is running, but image has CRITICAL CVE
	res := r.MatchProcessToVulnerabilities(ctx, "/bin/unrelated")
	if res.Strength != StrengthWeak {
		t.Errorf("Expected WEAK, got %v", res.Strength)
	}
}
