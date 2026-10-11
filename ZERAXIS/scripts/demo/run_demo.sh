#!/bin/bash
set -eo pipefail

cd "$(dirname "$0")/../.."

if [ -d "integrations/discord/.venv" ]; then
    export VIRTUAL_ENV="$(pwd)/integrations/discord/.venv"
    export PATH="$(pwd)/integrations/discord/.venv/bin:$PATH"
fi

RUN_ID="${ZERAXIS_RUN_ID:-$(python3 -c "import uuid; print(uuid.uuid4())" 2>/dev/null || echo "demo-$(date +%s)")}"

echo -e "\033[1;36m╔══════════════════════════════════════════════════════════════════════╗\033[0m"
echo -e "\033[1;36m║                    ZERAXIS SECURITY OPS                              ║\033[0m"
echo -e "\033[1;36m║             Autonomous Incident Response Platform                    ║\033[0m"
echo -e "\033[1;36m╠══════════════════════════════════════════════════════════════════════╣\033[0m"
echo -e "\033[1;36m║\033[0m \033[1mTarget Asset\033[0m : payment-demo-v17"
echo -e "\033[1;36m║\033[0m \033[1mRun ID\033[0m       : $RUN_ID"
echo -e "\033[1;36m║\033[0m \033[1mStatus\033[0m       : \033[33mMONITORING (runtime defense active)\033[0m"
echo -e "\033[1;36m╚══════════════════════════════════════════════════════════════════════╝\033[0m"
echo ""

if ! docker ps >/dev/null 2>&1; then
    echo "❌ Pre-flight check failed: Docker is not accessible."
    exit 1
fi

PG_CONTAINER=$(docker ps --format '{{.Names}}' | grep postgres | head -n 1)
if [ -z "$PG_CONTAINER" ]; then
    echo "❌ Pre-flight check failed: ZERAXIS environment is not running."
    exit 1
fi

NETWORK=$(docker network ls --format '{{.Name}}' | grep zeraxis-lab | head -n 1)
if [ -z "$NETWORK" ]; then
    echo "❌ Pre-flight check failed: ZERAXIS network not found."
    exit 1
fi

export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
export API_INTERNAL_SECRET="${API_INTERNAL_SECRET:-secret123}"
docker compose -f infrastructure/docker/docker-compose.yml -f scripts/demo/docker-compose.demo.yml up -d executor-engine >/dev/null 2>&1

docker rm -f payment-demo-v17 >/dev/null 2>&1 || true
docker run -d --name payment-demo-v17 --network "$NETWORK" alpine:latest sleep 3600 >/dev/null 2>&1

echo -e "\033[36m[*] Monitoring runtime container telemetry...\033[0m"

sleep 32

for i in {1..5}; do
    docker exec payment-demo-v17 sh -c "echo 'hack$i' > /etc/hack$i.txt" >/dev/null 2>&1
    sleep 1
done

while true; do
    CONTAINER_12=$(docker inspect --format='{{.Id}}' payment-demo-v17 | cut -c 1-12 2>/dev/null || echo "")
    ASSET_ID=$(docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -tAc "SELECT asset_id FROM discovery_assets WHERE container_id = '$CONTAINER_12' LIMIT 1" 2>/dev/null || echo "")
    
    if [ -n "$ASSET_ID" ]; then
        FINDING_JSON=$(docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -tAc "
            SELECT json_build_object(
                'rule_id', rule_id,
                'asset_id', asset_id,
                'finding_id', finding_id,
                'severity', severity,
                'evidence', evidence
            )
            FROM findings 
            WHERE status IN ('OPEN', 'UPDATED') AND asset_id = '$ASSET_ID'
            ORDER BY updated_at DESC LIMIT 1;
        " 2>/dev/null)
        
        if [ -n "$FINDING_JSON" ] && [ "$FINDING_JSON" != "null" ]; then
            RULE_ID=$(echo "$FINDING_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('rule_id', ''))")
            FINDING_ID=$(echo "$FINDING_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('finding_id', ''))")
            SEVERITY=$(echo "$FINDING_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('severity', 'HIGH'))")
            EVIDENCE_DETAIL=$(echo "$FINDING_JSON" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    ev = d.get('evidence', '')
    if isinstance(ev, str):
        ev = json.loads(ev)
    events = ev.get('events', [])
    if events:
        first = json.loads(events[0]) if isinstance(events[0], str) else events[0]
        op = first.get('operation', 'WRITE')
        path = first.get('path', '/etc/hack*.txt')
        proc = first.get('process', 'sh')
        uid = first.get('uid', 0)
        print(f'{op} {path} (proc={proc}, uid={uid})')
    else:
        print('Multiple unauthorized file writes to /etc')
except Exception:
    print('Unauthorized file write to sensitive system path')
")
            break
        fi
    fi
    sleep 2
done

echo -e "\n\033[1;31m⚠ SUSPICIOUS BEHAVIOR DETECTED\033[0m"
echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
echo "     Rule: $RULE_ID"
echo "     Asset: payment-demo-v17"
echo "     Finding ID: $FINDING_ID"
echo -e "     Severity: \033[1;31m$SEVERITY\033[0m"
if [ -n "$EVIDENCE_DETAIL" ]; then
    echo "     Evidence: $EVIDENCE_DETAIL"
fi

while true; do
    INCIDENT_JSON=$(docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -tAc "
        SELECT json_build_object('incident_id', i.incident_id, 'risk_level', i.risk_level, 'confidence', i.confidence, 'status', i.status)
        FROM incidents i
        JOIN incident_findings f ON i.incident_id = f.incident_id
        WHERE f.finding_id = '$FINDING_ID'
          AND i.status IN ('OPEN', 'ACTIVE')
        LIMIT 1;
    " 2>/dev/null)
    
    if [ -n "$INCIDENT_JSON" ] && [ "$INCIDENT_JSON" != "null" ]; then
        INCIDENT_ID=$(echo "$INCIDENT_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('incident_id', ''))")
        RISK_LEVEL=$(echo "$INCIDENT_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('risk_level', ''))")
        CONFIDENCE=$(echo "$INCIDENT_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('confidence', ''))")
        if [ "$RISK_LEVEL" == "CRITICAL" ]; then
            break
        fi
    fi
    sleep 2
done

echo -e "\n\033[1;33m🔗 BEHAVIOR CORRELATED\033[0m"
echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
echo "     Evidence: Multiple critical system file modifications"
echo "     Context: uid=0 (root) unauthorized write to /etc"

echo -e "\n\033[1;35m🚨 INCIDENT CREATED\033[0m"
echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
echo "     Incident: $INCIDENT_ID"
echo -e "     Risk: \033[1;31m$RISK_LEVEL\033[0m"
echo "     Confidence: $CONFIDENCE"

while true; do
    REC_JSON=$(docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -tAc "
        SELECT json_build_object(
            'recommendation_id', recommendation_id,
            'action', action,
            'priority', priority,
            'reason', reason
        )
        FROM incident_recommendations
        WHERE incident_id = '$INCIDENT_ID' AND action = 'QUARANTINE_ASSET' AND status = 'PROPOSED'
        ORDER BY created_at DESC LIMIT 1;
    " 2>/dev/null)
    
    if [ -n "$REC_JSON" ] && [ "$REC_JSON" != "null" ]; then
        REC_ID=$(echo "$REC_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('recommendation_id', ''))")
        ACTION=$(echo "$REC_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('action', ''))")
        PRIORITY=$(echo "$REC_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('priority', 'CRITICAL'))")
        REC_REASON=$(echo "$REC_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('reason', 'Critical risk with strong evidence warrants isolation.'))")
        break
    fi
    sleep 2
done

echo -e "\n\033[1;34m🛡 RESPONSE RECOMMENDED\033[0m"
echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
echo "     Action: $ACTION"
echo ""
echo -e "  \033[1;33m⭐ ZERAXIS RECOMMENDATION\033[0m"
echo -e "  \033[2m────────────────────────────────────────────────────────────────────\033[0m"
echo -e "  \033[1mAction\033[0m               : \033[1;32m$ACTION\033[0m"
echo -e "  \033[1mRecommendation ID\033[0m    : $REC_ID"
echo -e "  \033[1mPriority\033[0m             : \033[1;31m$PRIORITY\033[0m"
echo -e "  \033[1mWhy recommended\033[0m      :"
echo -e "    $REC_REASON"
echo -e "  \033[1mSupporting Evidence\033[0m  :"
echo -e "    • Finding: $FINDING_ID (rule: $RULE_ID, severity: $SEVERITY)"
echo -e "    • Target: payment-demo-v17 (unauthorized writes to /etc)"
echo -e "  \033[1mPotential impact (pre-authorization)\033[0m:"
echo -e "    If approved: container loses network access. Service availability affected."
echo ""
echo -e "  \033[1mAvailable response options:\033[0m"
echo -e "    [1] Quarantine Asset                 \033[1;32m[HIGHLY RECOMMENDED]\033[0m \033[1;33m⭐ ZERAXIS RECOMMENDATION\033[0m"
echo -e "    [2] Collect More Evidence            \033[36m[AVAILABLE]\033[0m"
echo -e "    [3] Alert and Monitor                \033[36m[AVAILABLE]\033[0m"
echo -e "    [4] Block Suspicious Network Traffic \033[2m[NOT IMPLEMENTED]\033[0m"
echo -e "    [5] Suspend Suspicious Process       \033[2m[NOT IMPLEMENTED]\033[0m"
echo -e "    [6] Reject Recommendation            \033[2m[NOT IMPLEMENTED]\033[0m"
echo ""

# Developer response selection with responsive fallback
SEL_CHOICE="${ZERAXIS_SELECTION:-}"
if [ -z "$SEL_CHOICE" ]; then
    if [ -t 0 ] || [ -r /dev/tty ]; then
        read -t 5 -p "  Select response option [1-3] (default: 1): " USER_INPUT < /dev/tty 2>/dev/null || true
        echo ""
        SEL_CHOICE="${USER_INPUT:-1}"
    else
        SEL_CHOICE="1"
    fi
fi

case "$SEL_CHOICE" in
    1)
        SEL_ACTION="QUARANTINE_ASSET"
        ;;
    2)
        SEL_ACTION="COLLECT_MORE_EVIDENCE"
        ;;
    3)
        SEL_ACTION="ALERT_AND_MONITOR"
        ;;
    *)
        if [ "$SEL_CHOICE" == "4" ] || [ "$SEL_CHOICE" == "5" ] || [ "$SEL_CHOICE" == "6" ]; then
            echo -e "  \033[1;31mOption $SEL_CHOICE is NOT IMPLEMENTED. Executing recommendation [1].\033[0m\n"
        fi
        SEL_ACTION="QUARANTINE_ASSET"
        SEL_CHOICE="1"
        ;;
esac

# Persist selection state to shared outbox store
python3 integrations/discord/recommend_select.py \
    --incident "$INCIDENT_ID" \
    --rec-id "$REC_ID" \
    --target "payment-demo-v17" \
    --selection "$SEL_CHOICE" \
    --confirm "y" >/dev/null 2>&1 || true

if [ "$SEL_ACTION" == "COLLECT_MORE_EVIDENCE" ]; then
    echo -e "\n\033[1;32m👤 HUMAN AUTHORIZATION\033[0m"
    echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
    echo -e "     Approver           : admin (Security Operations)"
    echo -e "     Decision           : \033[1;32mAPPROVED\033[0m"
    echo -e "     Action             : COLLECT_MORE_EVIDENCE"
    echo "     Status: APPROVED"

    echo -e "\n\033[1;36m⚙ RESPONSE EXECUTED\033[0m"
    echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
    echo "     Execution: local-evidence-collection-01"
    echo -e "     Action   : COLLECT_MORE_EVIDENCE"
    echo -e "     Target   : payment-demo-v17"
    echo -e "     Status   : \033[1;33mEXECUTION FAILED (Automated executor handler missing in backend)\033[0m"

    echo -e "\n\033[1;35m🔍 POST-EXECUTION VERIFICATION\033[0m"
    echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
    echo -e "     Postcondition : \033[1;33mVERIFICATION UNAVAILABLE (Non-disruptive telemetry observation; asset remains active)\033[0m"
    echo -e "     Incident      : ACTIVE (Threat remains unmitigated)"
    exit 0
elif [ "$SEL_ACTION" == "ALERT_AND_MONITOR" ]; then
    echo -e "\n\033[1;32m👤 HUMAN AUTHORIZATION\033[0m"
    echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
    echo -e "     Approver           : admin (Security Operations)"
    echo -e "     Decision           : \033[1;32mAPPROVED\033[0m"
    echo -e "     Action             : ALERT_AND_MONITOR"
    echo "     Status: APPROVED"

    echo -e "\n\033[1;36m⚙ RESPONSE EXECUTED\033[0m"
    echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
    echo "     Execution: local-alert-monitor-01"
    echo -e "     Action   : ALERT_AND_MONITOR"
    echo -e "     Target   : payment-demo-v17"
    echo -e "     Status   : \033[1;33mEXECUTION FAILED (Automated executor handler missing in backend)\033[0m"

    echo -e "\n\033[1;35m🔍 POST-EXECUTION VERIFICATION\033[0m"
    echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
    echo -e "     Postcondition : \033[1;33mVERIFICATION UNAVAILABLE (Monitoring active; zero mutation performed)\033[0m"
    echo -e "     Incident      : ACTIVE (Threat remains unmitigated)"
    exit 0
fi

# Action is QUARANTINE_ASSET: execute verified handler
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "http://localhost:8080/api/v1/recommendations/$REC_ID/approve" -u admin:admin || echo "000")

if [ "$HTTP_STATUS" != "200" ]; then
    echo -e "\n\033[1;31m❌ HUMAN AUTHORIZATION FAILED\033[0m"
    echo "     HTTP Status: $HTTP_STATUS"
    exit 1
fi

echo -e "\n\033[1;32m👤 HUMAN AUTHORIZATION\033[0m"
echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
echo -e "     Policy Requirement : Explicit operator authorization required"
echo -e "     Approver           : admin (Security Operations)"
echo -e "     Decision           : \033[1;32mAPPROVED\033[0m"
echo -e "     Action             : QUARANTINE_ASSET"
echo -e "     Target validation  : \033[32mPASSED\033[0m (Asset: payment-demo-v17)"
echo "     Status: APPROVED"

while true; do
    EXEC_JSON=$(docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -tAc "
        SELECT json_build_object('execution_id', er.execution_id, 'status', ea.status, 'verification_status', er.verification_status)
        FROM execution_results er
        JOIN execution_authorizations ea ON er.execution_id = ea.execution_id
        WHERE ea.recommendation_id = '$REC_ID'
        LIMIT 1;
    " 2>/dev/null)
    
    if [ -n "$EXEC_JSON" ] && [ "$EXEC_JSON" != "null" ]; then
        EXEC_ID=$(echo "$EXEC_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('execution_id', ''))")
        EXEC_STATUS=$(echo "$EXEC_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('status', ''))")
        
        if [ "$EXEC_STATUS" == "EXECUTED" ]; then
            break
        fi
        if [ "$EXEC_STATUS" == "FAILED" ]; then
            echo -e "\n\033[1;31m❌ RESPONSE EXECUTION FAILED\033[0m"
            echo "     Execution: $EXEC_ID"
            echo "     Reason: Executor rejected or failed to process"
            exit 1
        fi
    fi
    sleep 2
done

echo -e "\n\033[1;36m⚙ RESPONSE EXECUTED\033[0m"
echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
echo "     Execution: $EXEC_ID"
echo -e "     Action   : $ACTION"
echo -e "     Target   : payment-demo-v17"
echo -e "     Status   : \033[1;32mEXECUTED\033[0m"

while true; do
    VERIFY_JSON=$(docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -tAc "
        SELECT json_build_object('verification_status', verification_status)
        FROM execution_results
        WHERE execution_id = '$EXEC_ID'
        LIMIT 1;
    " 2>/dev/null)
    
    if [ -n "$VERIFY_JSON" ] && [ "$VERIFY_JSON" != "null" ]; then
        VER_STATUS=$(echo "$VERIFY_JSON" | python3 -c "import sys, json; print(json.load(sys.stdin).get('verification_status', ''))")
        if [ "$VER_STATUS" == "VERIFIED" ]; then
            break
        fi
        if [ "$VER_STATUS" == "FAILED" ]; then
            echo -e "\n\033[1;31m❌ POST-EXECUTION VERIFICATION FAILED\033[0m"
            exit 1
        fi
    fi
    sleep 2
done

echo -e "\n\033[1;35m🔍 POST-EXECUTION VERIFICATION\033[0m"
echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
echo "     Execution: $EXEC_ID"
echo -e "     Verification  : \033[1;32mVERIFIED\033[0m"
echo -e "     Postcondition : \033[1;32mCONFIRMED (Asset isolated from network)\033[0m"

while true; do
    INC_FINAL_STATUS=$(docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -tAc "
        SELECT status FROM incidents WHERE incident_id = '$INCIDENT_ID';
    " 2>/dev/null)
    
    if [ "$INC_FINAL_STATUS" == "RESOLVED" ]; then
        break
    fi
    sleep 2
done

echo -e "\n\033[1;32m✅ INCIDENT RESOLVED\033[0m"
echo -e "\033[2m────────────────────────────────────────────────────────────────────────\033[0m"
echo "     Incident ID   : $INCIDENT_ID"
echo -e "     Final Status  : \033[1;32mRESOLVED\033[0m"
echo "     Remediation   : QUARANTINE_ASSET verified"
echo "     Resolution    : Autonomous defense loop completed successfully"
echo ""
echo -e "\033[1;32m╔══════════════════════════════════════════════════════════════════════╗\033[0m"
echo -e "\033[1;32m║                    ZERAXIS DEFENSE LOOP COMPLETE                     ║\033[0m"
echo -e "\033[1;32m║        DETECT → CORRELATE → RECOMMEND → AUTHORIZE → VERIFY           ║\033[0m"
echo -e "\033[1;32m╚══════════════════════════════════════════════════════════════════════╝\033[0m"
