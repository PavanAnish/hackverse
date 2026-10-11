# ZERAXIS Phase 6 Risk Model

## Mathematical Formula (Version 1.0)
Phase 6 evaluates Security Incident risk deterministically without opaque AI/ML.

### 1. Static Risk
Base formula:
`StaticRisk = MIN(100, BaseCVSS_Severity * AssetCriticalityMultiplier + PolicyBlockModifier)`

- **BaseCVSS_Severity**: Highest CVE severity mapped to a score (CRITICAL=80, HIGH=60, MEDIUM=40, LOW=20, INFO=0).
- **AssetCriticalityMultiplier**: CRITICAL=1.2, HIGH=1.1, MEDIUM=1.0, LOW=0.9, UNKNOWN=1.0.
- **PolicyBlockModifier**: +20 if Phase 3 Policy Decision is BLOCK.

### 2. Dynamic Risk
Base formula:
`DynamicRisk = MIN(100, BaseFindingSeverity * TemporalDensityMultiplier * AttackChainModifier)`

- **BaseFindingSeverity**: Max finding severity (CRITICAL=80, HIGH=60, MEDIUM=40, LOW=20).
- **TemporalDensityMultiplier**: 1.0 + (MIN(20, FindingCount) * 0.05). Maximum 2.0x.
- **AttackChainModifier**: 
  - EXPOSURE: 1.0
  - EXECUTION: 1.25
  - NETWORK_ACTIVITY: 1.5
  - IMPACT: 2.0

### 3. Combined Risk
`CombinedRisk = MIN(100, (StaticRisk * 0.4) + (DynamicRisk * 0.6))`

### 4. Risk Decay
- Static Risk never decays (unless `image_digest` is updated resolving the CVE).
- Dynamic Risk decays linearly by 5% every hour after 2 hours of inactivity, bottoming out at 0.

### 5. Confidence
- Confidence is a separate integer (0-100).
- Baseline: 100
- Modifier: If context mapping is MEDIUM (x0.7), WEAK (x0.4), UNKNOWN (x0.1).
