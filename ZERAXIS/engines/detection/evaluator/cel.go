package evaluator

import (
	"encoding/json"
	"fmt"
	
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/pavanvanish/zeraxis/engines/detection/models"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

type CompiledStep struct {
	Step    models.SequenceStep
	Program cel.Program
}

type CompiledRule struct {
	Rule    models.Rule
	Program cel.Program
	Steps   []CompiledStep
}

type Engine struct {
	env      *cel.Env
	rules    []CompiledRule
}

func NewEngine(rules []models.Rule) (*Engine, error) {
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewVar("event", decls.NewMapType(decls.String, decls.Dyn)),
			decls.NewVar("sequence", decls.NewListType(decls.NewMapType(decls.String, decls.Dyn))),
			decls.NewVar("supply_chain", decls.NewMapType(decls.String, decls.Dyn)),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL env: %w", err)
	}

	var compiled []CompiledRule
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		
		cr := CompiledRule{Rule: r}

		if len(r.Sequence) > 0 {
			for _, step := range r.Sequence {
				ast, iss := env.Compile(step.Condition)
				if iss.Err() != nil {
					return nil, fmt.Errorf("failed to compile step %s in rule %s: %w", step.Name, r.RuleID, iss.Err())
				}
				prg, err := env.Program(ast)
				if err != nil {
					return nil, err
				}
				cr.Steps = append(cr.Steps, CompiledStep{Step: step, Program: prg})
			}
		} else {
			ast, iss := env.Compile(r.Conditions)
			if iss.Err() != nil {
				return nil, fmt.Errorf("failed to compile stateless rule %s: %w", r.RuleID, iss.Err())
			}
			prg, err := env.Program(ast)
			if err != nil {
				return nil, err
			}
			cr.Program = prg
		}
		
		compiled = append(compiled, cr)
	}

	return &Engine{env: env, rules: compiled}, nil
}

func EventToMap(evt events.Event) (map[string]interface{}, error) {
	b, err := json.Marshal(evt)
	if err != nil {
		return nil, err
	}
	var eventMap map[string]interface{}
	if err := json.Unmarshal(b, &eventMap); err != nil {
		return nil, err
	}
	return eventMap, nil
}

func (e *Engine) EvaluateStateless(evt events.Event, scContext map[string]interface{}) ([]models.Rule, error) {
	eventMap, err := EventToMap(evt)
	if err != nil {
		return nil, err
	}

	if scContext == nil {
		scContext = map[string]interface{}{"unknown": true}
	}

	celInput := map[string]interface{}{
		"event":        eventMap,
		"sequence":     []map[string]interface{}{},
		"supply_chain": scContext,
	}

	var matches []models.Rule

	for _, cr := range e.rules {
		if len(cr.Steps) > 0 {
			continue
		}
		typeMatches := false
		for _, t := range cr.Rule.RequiredEventTypes {
			if t == evt.EventType || t == "*" {
				typeMatches = true
				break
			}
		}
		if !typeMatches {
			continue
		}

		out, _, err := cr.Program.Eval(celInput)
		if err != nil {
			continue
		}

		if out.Type() == types.BoolType && out.Value().(bool) {
			matches = append(matches, cr.Rule)
		}
	}

	return matches, nil
}

func (e *Engine) EvaluateTemporalStep(ruleID string, stepIndex int, evt events.Event, sequence []events.Event, scContext map[string]interface{}) (bool, error) {
	var cr *CompiledRule
	for _, r := range e.rules {
		if r.Rule.RuleID == ruleID {
			cr = &r
			break
		}
	}
	if cr == nil || stepIndex >= len(cr.Steps) {
		return false, fmt.Errorf("rule or step not found")
	}

	step := cr.Steps[stepIndex]
	if step.Step.EventType != "*" && evt.EventType != step.Step.EventType {
		return false, nil
	}

	eventMap, err := EventToMap(evt)
	if err != nil {
		return false, err
	}

	seqMaps := make([]map[string]interface{}, len(sequence))
	for i, seqEvt := range sequence {
		m, err := EventToMap(seqEvt)
		if err != nil {
			return false, err
		}
		seqMaps[i] = m
	}

	if scContext == nil {
		scContext = map[string]interface{}{"unknown": true}
	}

	celInput := map[string]interface{}{
		"event":        eventMap,
		"sequence":     seqMaps,
		"supply_chain": scContext,
	}

	out, _, err := step.Program.Eval(celInput)
	if err != nil {
		return false, err
	}

	if out.Type() == types.BoolType && out.Value().(bool) {
		return true, nil
	}
	return false, nil
}

func (e *Engine) GetTemporalRules() []models.Rule {
	var tr []models.Rule
	for _, cr := range e.rules {
		if len(cr.Steps) > 0 {
			tr = append(tr, cr.Rule)
		}
	}
	return tr
}
