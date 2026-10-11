const fs = require('fs');
let content = fs.readFileSync('ai-agent/remediation.py', 'utf8');

const oldEmbed = `    elif status == "pending_approval":
        embed = {
            "title": "⏳ Approval Required",
            "color": 10181046, # Purple
            "fields": [
                {"name": "Service", "value": service, "inline": True},
                {"name": "Recommended Action", "value": action, "inline": True},
                {"name": "Root Cause", "value": root_cause, "inline": False},
                {"name": "Status", "value": f"AI recommends {action} for {service} — approval needed", "inline": False}
            ]
        }`;

const newEmbed = `    elif status == "pending_approval":
        embed = {
            "title": "⏳ Approval Required",
            "color": 10181046, # Purple
            "fields": [
                {"name": "Service", "value": service, "inline": True},
                {"name": "Recommended Action", "value": action, "inline": True},
                {"name": "Root Cause", "value": root_cause, "inline": False},
                {"name": "Status", "value": f"AI recommends {action} for {service} — approval needed", "inline": False},
                {"name": "Action Required", "value": "[Open Dashboard to Approve](http://localhost:3001)", "inline": False}
            ]
        }`;

content = content.replace(oldEmbed, newEmbed);
fs.writeFileSync('ai-agent/remediation.py', content);
