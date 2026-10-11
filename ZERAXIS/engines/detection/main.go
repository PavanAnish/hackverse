package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/engines/detection/evaluator"
	"github.com/pavanvanish/zeraxis/engines/detection/fingerprint"
	"github.com/pavanvanish/zeraxis/engines/detection/models"
	"github.com/pavanvanish/zeraxis/engines/detection/repository"
	"github.com/pavanvanish/zeraxis/engines/detection/server"
	"github.com/pavanvanish/zeraxis/engines/detection/state"
	zctx "github.com/pavanvanish/zeraxis/engines/detection/context"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func main() {
	slog.Info("Starting Detection Engine (Stage 2A + 2B + 2C)")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		slog.Error("Failed to open db", "err", err)
		os.Exit(1)
	}
	repo := repository.New(db)
	if err := repo.EnsureSchema(context.Background()); err != nil {
		slog.Error("Failed to ensure schema", "err", err)
		os.Exit(1)
	}

	apiServer := server.New(db)
	apiServer.Start()

	resolver := zctx.NewResolver(db)

	rules := []models.Rule{
		{
			RuleID: "rule-pkg-mgr-exec", RuleVersion: 1, Name: "PACKAGE_MANAGER_EXECUTION", Severity: "MEDIUM", Confidence: "HIGH", Enabled: true,
			RequiredEventTypes: []string{"PROCESS_EXECUTION"},
			Conditions: `has(event.metadata.process) && event.metadata.process in ["apt", "apt-get", "apk", "yum", "dnf", "rpm", "dpkg"]`,
			GroupByFields: []string{"asset_id", "container_id", "image_digest"},
		},
		{
			RuleID: "rule-unexpected-shell", RuleVersion: 1, Name: "UNEXPECTED_SHELL", Severity: "HIGH", Confidence: "HIGH", Enabled: true,
			RequiredEventTypes: []string{"PROCESS_EXECUTION"},
			Conditions: `has(event.metadata.process) && event.metadata.process in ["sh", "bash", "ash", "zsh"]`,
			GroupByFields: []string{"asset_id", "container_id", "image_digest"},
		},
		{
			RuleID: "rule-sensitive-file-write", RuleVersion: 1, Name: "SENSITIVE_FILE_WRITE", Severity: "CRITICAL", Confidence: "HIGH", Enabled: true,
			RequiredEventTypes: []string{"FILE_ACTIVITY"},
			Conditions: `has(event.metadata.operation) && event.metadata.operation == "WRITE" && has(event.metadata.path) && (event.metadata.path.startsWith("/etc/") || event.metadata.path.startsWith("/var/run/"))`,
			GroupByFields: []string{"asset_id", "container_id", "image_digest"},
		},
		{
			RuleID: "rule-writable-dir-exec", RuleVersion: 1, Name: "WRITABLE_DIRECTORY_EXECUTION", Severity: "HIGH", Confidence: "HIGH", Enabled: true,
			RequiredEventTypes: []string{"PROCESS_EXECUTION"},
			Conditions: `has(event.metadata.path) && (event.metadata.path.startsWith("/tmp/") || event.metadata.path.startsWith("/dev/shm/"))`,
			GroupByFields: []string{"asset_id", "container_id", "image_digest"},
		},
		{
			RuleID: "rule-vuln-exec", RuleVersion: 1, Name: "Vulnerable Package Execution", Severity: "HIGH", Confidence: "MEDIUM", Enabled: true,
			RequiredEventTypes: []string{"PROCESS_EXECUTION"},
			Conditions: `has(supply_chain.match) && (supply_chain.match.strength == "STRONG" || supply_chain.match.strength == "MEDIUM")`,
			GroupByFields: []string{"asset_id", "image_digest"},
		},
		{
			RuleID: "rule-vuln-write", RuleVersion: 1, Name: "Vulnerable Package File Write", Severity: "HIGH", Confidence: "MEDIUM", Enabled: true,
			RequiredEventTypes: []string{"FILE_ACTIVITY"},
			Conditions: `event.metadata.operation == "WRITE" && has(supply_chain.match) && supply_chain.match.strength == "STRONG"`,
			GroupByFields: []string{"asset_id", "image_digest"},
		},
		{
			RuleID: "rule-policy-block-exec", RuleVersion: 1, Name: "Blocked Image Execution", Severity: "CRITICAL", Confidence: "HIGH", Enabled: true,
			RequiredEventTypes: []string{"PROCESS_EXECUTION"},
			Conditions: `has(supply_chain.policy) && supply_chain.policy.result == "BLOCK"`,
			GroupByFields: []string{"asset_id", "image_digest"},
		},
		{
			RuleID: "rule-download-execute", RuleVersion: 1, Name: "Download and Execute", Severity: "CRITICAL", Confidence: "HIGH", Enabled: true,
			TimeWindow: 10 * time.Second,
			Correlation: models.Correlation{Fields: []string{"asset_id", "container_id"}},
			Sequence: []models.SequenceStep{
				{Name: "network", EventType: "NETWORK_CONNECTION", Condition: `event.metadata.direction == "egress" && !event.metadata.destination_ip.startsWith("10.")`},
				{Name: "download", EventType: "FILE_ACTIVITY", Condition: `event.metadata.operation == "WRITE" && event.metadata.path.startsWith("/tmp/")`},
				{Name: "execute", EventType: "PROCESS_EXECUTION", Condition: `has(sequence[1].metadata.path) && has(event.metadata.executable) && event.metadata.executable == sequence[1].metadata.path`},
			},
		},
		{
			RuleID: "rule-vuln-net", RuleVersion: 1, Name: "Vulnerability Network Exposure", Severity: "CRITICAL", Confidence: "HIGH", Enabled: true,
			TimeWindow: 10 * time.Second,
			Correlation: models.Correlation{Fields: []string{"asset_id", "container_id"}},
			Sequence: []models.SequenceStep{
				{Name: "exec", EventType: "PROCESS_EXECUTION", Condition: `has(supply_chain.match) && supply_chain.match.strength == "STRONG"`},
				{Name: "net", EventType: "NETWORK_CONNECTION", Condition: `has(event.metadata.process) && has(sequence[0].metadata.process) && event.metadata.process == sequence[0].metadata.process && event.metadata.direction == "egress"`},
			},
		},
	}

	engine, err := evaluator.NewEngine(rules)
	if err != nil {
		slog.Error("Failed to build CEL engine", "err", err)
		os.Exit(1)
	}
	
	stateMgr := state.NewManager(engine)

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		slog.Error("Failed to connect to NATS", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	processedEvents := make(map[string]bool)

	sub, err := nc.Subscribe("zeraxis.events.runtime.>", func(m *nats.Msg) {
		var evt events.Event
		if err := json.Unmarshal(m.Data, &evt); err != nil {
			return
		}

		if processedEvents[evt.EventID] {
			return // Duplicate
		}
		processedEvents[evt.EventID] = true
		if len(processedEvents) > 5000 {
			processedEvents = make(map[string]bool)
		}

		ctxData := map[string]interface{}{"unknown": true}
		
		if evt.ImageDigest != "" {
			imgCtx := resolver.GetContext(context.Background(), evt.ImageDigest)
			if !imgCtx.Unknown {
				ctxData["unknown"] = false
				ctxData["policy"] = map[string]interface{}{
					"result":         imgCtx.PolicyResult,
					"critical_count": imgCtx.PolicyCritical,
				}
				
				executable := ""
				var raw map[string]interface{}
				if json.Unmarshal(evt.Metadata, &raw) == nil {
					if ex, ok := raw["executable"].(string); ok {
						executable = ex
					} else if p, ok := raw["process"].(string); ok {
						executable = p
					}
				}

				if executable != "" {
					matchResult := resolver.MatchProcessToVulnerabilities(imgCtx, executable)
					ctxData["match"] = map[string]interface{}{
						"strength":    string(matchResult.Strength),
						"explanation": matchResult.Explanation,
					}
					var vulns []map[string]interface{}
					for _, v := range matchResult.Vulnerabilities {
						vulns = append(vulns, map[string]interface{}{
							"cve":      v.Identifier,
							"severity": v.Severity,
							"package":  v.PackageName,
						})
					}
					ctxData["match"].(map[string]interface{})["vulnerabilities"] = vulns
				}
			}
		}

		matches, _ := engine.EvaluateStateless(evt, ctxData)
		processMatches(matches, []events.Event{evt}, repo, nc, ctxData)

		completedSeqs := stateMgr.Receive(evt, ctxData)
		for _, seq := range completedSeqs {
			processMatches([]models.Rule{seq.Rule}, seq.Matched, repo, nc, ctxData)
		}
	})

	if err != nil {
		slog.Error("Failed to subscribe", "err", err)
		os.Exit(1)
	}
	defer sub.Unsubscribe()

	slog.Info("Detection engine running with Stage 2C Contextual Correlation. Waiting for events...")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

func processMatches(matches []models.Rule, evts []events.Event, repo *repository.Repository, nc *nats.Conn, scContext map[string]interface{}) {
	for _, r := range matches {
		if len(evts) == 0 {
			continue
		}
		
		lastEvt := evts[len(evts)-1]
		
		groupMap := make(map[string]string)
		b, _ := json.Marshal(lastEvt)
		var raw map[string]interface{}
		json.Unmarshal(b, &raw)
		
		for _, field := range r.GroupByFields {
			parts := strings.Split(field, ".")
			var val interface{} = raw
			for _, p := range parts {
				if m, ok := val.(map[string]interface{}); ok {
					val = m[p]
				} else {
					val = nil
				}
			}
			if val != nil {
				groupMap[field] = fmt.Sprintf("%v", val)
			}
		}

		fp := fingerprint.Generate(r.RuleID, r.RuleVersion, lastEvt.AssetID, lastEvt.ImageDigest, groupMap)
		
		var evidence []string
		for _, e := range evts {
			mB, _ := json.Marshal(e.Metadata)
			evidence = append(evidence, string(mB))
		}
		
		evidenceObj := map[string]interface{}{
			"events": evidence,
		}
		if scContext != nil && scContext["unknown"] == false {
			evidenceObj["supply_chain_context"] = scContext
		}
		
		evidenceB, _ := json.Marshal(evidenceObj)
		evidenceStr := string(evidenceB)

		f := models.Finding{
			Fingerprint: fp,
			RuleID:      r.RuleID,
			RuleVersion: r.RuleVersion,
			Severity:    r.Severity,
			Confidence:  r.Confidence,
			AssetID:     lastEvt.AssetID,
			ImageDigest: lastEvt.ImageDigest,
			FirstSeen:   evts[0].Timestamp,
			LastSeen:    lastEvt.Timestamp,
			Evidence:    evidenceStr,
		}

		fid, isNew, err := repo.UpsertFinding(context.Background(), f, lastEvt.EventID)
		if err != nil {
			slog.Error("Failed to upsert finding", "err", err)
			continue
		}
		
		for _, e := range evts {
			repo.UpsertFinding(context.Background(), f, e.EventID)
		}

		// Extract container_id from the last event's metadata so the correlator
		// can propagate it to the incident and recommendation target fields.
		var containerID string
		var metaMap map[string]interface{}
		if err := json.Unmarshal(lastEvt.Metadata, &metaMap); err == nil {
			if c, ok := metaMap["container_id"].(string); ok {
				containerID = c
			}
		}

		fe := models.FindingEvent{
			FindingID:         fid,
			Status:            "OPEN",
			RuleID:            r.RuleID,
			Severity:          r.Severity,
			AssetID:           lastEvt.AssetID,
			ImageDigest:       lastEvt.ImageDigest,
			ContainerID:       containerID,
			TriggeringEventID: lastEvt.EventID,
		}
		if !isNew {
			fe.Status = "UPDATED"
		}
		
		b, _ = json.Marshal(fe)
		nc.Publish("zeraxis.events.detection.finding."+strings.ToLower(fe.Status), b)
		
		slog.Info("Finding recorded", "rule_id", r.RuleID, "finding_id", fid, "events", len(evts))
	}
}
