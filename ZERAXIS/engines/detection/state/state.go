package state

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/pavanvanish/zeraxis/engines/detection/evaluator"
	"github.com/pavanvanish/zeraxis/engines/detection/models"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

const (
	MaxContainers           = 5000
	MaxSequencesPerCont     = 100
	AllowedLateness         = 2 * time.Second
	MaxClockSkew            = 5 * time.Second
)

type SequenceState struct {
	Rule        models.Rule
	StartTime   time.Time
	CurrentStep int
	Matched     []events.Event
}

type ContainerState struct {
	ContainerID      string
	HighestTimestamp time.Time
	Watermark        time.Time
	Buffer           []events.Event
	ActiveSequences  []*SequenceState
}

type Manager struct {
	Containers map[string]*ContainerState
	Engine     *evaluator.Engine
	
	// Metrics
	Metrics struct {
		Received int
		Dropped  int
		Active   int
		Evicted  int
		Started  int
		Completed int
		Expired  int
	}
}

func NewManager(engine *evaluator.Engine) *Manager {
	return &Manager{
		Containers: make(map[string]*ContainerState),
		Engine:     engine,
	}
}

func (m *Manager) Receive(evt events.Event, scContext map[string]interface{}) []*SequenceState {
	m.Metrics.Received++
	now := time.Now()

	var containerID string
	var mData map[string]interface{}
	if err := json.Unmarshal(evt.Metadata, &mData); err == nil {
		if c, ok := mData["container_id"].(string); ok {
			containerID = c
		}
	}
	
	if containerID == "" {
		if evt.AssetID != "" {
			containerID = evt.AssetID
		} else {
			m.Metrics.Dropped++
			return nil
		}
	}

	if evt.Timestamp.After(now.Add(MaxClockSkew)) {
		m.Metrics.Dropped++
		return nil
	}

	cs, exists := m.Containers[containerID]
	if !exists {
		if len(m.Containers) >= MaxContainers {
			for k := range m.Containers {
				delete(m.Containers, k)
				m.Metrics.Evicted++
				break
			}
		}
		cs = &ContainerState{
			ContainerID:      containerID,
			HighestTimestamp: evt.Timestamp,
			Watermark:        evt.Timestamp.Add(-AllowedLateness),
		}
		m.Containers[containerID] = cs
	}

	if evt.Timestamp.Before(cs.Watermark) {
		m.Metrics.Dropped++
		return nil
	}

	if evt.Timestamp.After(cs.HighestTimestamp) {
		cs.HighestTimestamp = evt.Timestamp
	}
	cs.Watermark = cs.HighestTimestamp.Add(-AllowedLateness)

	cs.Buffer = append(cs.Buffer, evt)
	sort.SliceStable(cs.Buffer, func(i, j int) bool {
		return cs.Buffer[i].Timestamp.Before(cs.Buffer[j].Timestamp)
	})

	var completed []*SequenceState

	for len(cs.Buffer) > 0 && !cs.Buffer[0].Timestamp.After(cs.Watermark) {
		readyEvent := cs.Buffer[0]
		cs.Buffer = cs.Buffer[1:]
		
		comps := m.evaluateStateMachine(cs, readyEvent, scContext)
		completed = append(completed, comps...)
	}

	return completed
}

func (m *Manager) evaluateStateMachine(cs *ContainerState, evt events.Event, scContext map[string]interface{}) []*SequenceState {
	var completed []*SequenceState
	var remaining []*SequenceState

	for _, seq := range cs.ActiveSequences {
		if evt.Timestamp.Sub(seq.StartTime) > seq.Rule.TimeWindow {
			m.Metrics.Expired++
			m.Metrics.Active--
			continue
		}

		matched, err := m.Engine.EvaluateTemporalStep(seq.Rule.RuleID, seq.CurrentStep, evt, seq.Matched, scContext)
		if err == nil && matched {
			seq.Matched = append(seq.Matched, evt)
			seq.CurrentStep++

			if seq.CurrentStep == len(seq.Rule.Sequence) {
				completed = append(completed, seq)
				m.Metrics.Completed++
				m.Metrics.Active--
			} else {
				remaining = append(remaining, seq)
			}
		} else {
			remaining = append(remaining, seq)
		}
	}
	cs.ActiveSequences = remaining

	for _, rule := range m.Engine.GetTemporalRules() {
		matched, err := m.Engine.EvaluateTemporalStep(rule.RuleID, 0, evt, nil, scContext)
		if err == nil && matched {
			if len(cs.ActiveSequences) >= MaxSequencesPerCont {
				m.Metrics.Dropped++
				continue
			}

			newSeq := &SequenceState{
				Rule:        rule,
				StartTime:   evt.Timestamp,
				CurrentStep: 1,
				Matched:     []events.Event{evt},
			}
			
			if len(rule.Sequence) == 1 {
				completed = append(completed, newSeq)
				m.Metrics.Completed++
			} else {
				cs.ActiveSequences = append(cs.ActiveSequences, newSeq)
				m.Metrics.Started++
				m.Metrics.Active++
			}
		}
	}

	return completed
}
