const fs = require('fs');
let content = fs.readFileSync('storefront/src/App.jsx', 'utf8');

// 1. Add lastIncidentId to App state
content = content.replace(
  "const [status, setStatus] = useState('idle');",
  "const [status, setStatus] = useState('idle');\n  const [lastIncidentId, setLastIncidentId] = useState(0);"
);

// 2. Update simulateOutage in App
const oldSimulateOutage = `const simulateOutage = async (enable, type = 'default') => {
    try {
      await fetch(\`http://localhost:8002/chaos/token\`, { 
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enable, type })
      });
      setDemoStatus(enable ? \`Outage Activated! (\${type === 'cache' ? 'Cache' : 'DB'} Locked)\` : 'Systems Restored');
      setTimeout(() => setDemoStatus(''), 4000);
    } catch (e) {
      console.error(e);
    }
  };`;

const newSimulateOutage = `const simulateOutage = async (enable, type = 'default') => {
    try {
      if (enable) {
        try {
          const res = await fetch("http://localhost:8000/api/incidents");
          if (res.ok) {
             const data = await res.json();
             if (data.length > 0) setLastIncidentId(data[0].id);
             else setLastIncidentId(0);
          }
        } catch(e) {}
      }
      await fetch(\`http://localhost:8002/chaos/token\`, { 
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enable, type })
      });
      setDemoStatus(enable ? \`Outage Activated! (\${type === 'cache' ? 'Cache' : 'DB'} Locked)\` : 'Systems Restored');
      setTimeout(() => setDemoStatus(''), 4000);
    } catch (e) {
      console.error(e);
    }
  };`;

content = content.replace(oldSimulateOutage, newSimulateOutage);

// 3. Pass lastIncidentId to PlansPage
content = content.replace("<PlansPage />", "<PlansPage lastIncidentId={lastIncidentId} />");

fs.writeFileSync('storefront/src/App.jsx', content);
