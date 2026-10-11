package policy

import (
	"strings"
)

type Action string

const (
	ActionBlock  Action = "BLOCK"
	ActionWarn   Action = "WARN"
	ActionRecord Action = "RECORD"
	ActionAllow  Action = "ALLOW"
)

// Precedence map for action resolution
var precedence = map[Action]int{
	ActionAllow:  0,
	ActionRecord: 1,
	ActionWarn:   2,
	ActionBlock:  3,
}

type Config struct {
	Critical Action
	High     Action
	Medium   Action
	Low      Action
}

func parseAction(s string, def Action) Action {
	switch strings.ToUpper(s) {
	case "BLOCK":
		return ActionBlock
	case "WARN":
		return ActionWarn
	case "RECORD":
		return ActionRecord
	case "ALLOW":
		return ActionAllow
	default:
		return def
	}
}

func NewConfig(critical, high, medium, low string) Config {
	return Config{
		Critical: parseAction(critical, ActionBlock),
		High:     parseAction(high, ActionWarn),
		Medium:   parseAction(medium, ActionRecord),
		Low:      parseAction(low, ActionRecord),
	}
}

// Evaluate determines the policy decision based on the vulnerability counts.
func (c *Config) Evaluate(critical, high, medium, low int) (Action, string) {
	highest := ActionAllow
	var reasons []string

	if critical > 0 {
		highest = resolveAction(highest, c.Critical)
		reasons = append(reasons, "CRITICAL vulnerabilities detected")
	}
	if high > 0 {
		highest = resolveAction(highest, c.High)
		reasons = append(reasons, "HIGH vulnerabilities detected")
	}
	if medium > 0 {
		highest = resolveAction(highest, c.Medium)
		reasons = append(reasons, "MEDIUM vulnerabilities detected")
	}
	if low > 0 {
		highest = resolveAction(highest, c.Low)
		reasons = append(reasons, "LOW vulnerabilities detected")
	}

	if highest == ActionAllow {
		return ActionAllow, "No actionable vulnerabilities detected"
	}

	return highest, strings.Join(reasons, "; ")
}

func resolveAction(current, proposed Action) Action {
	if precedence[proposed] > precedence[current] {
		return proposed
	}
	return current
}
