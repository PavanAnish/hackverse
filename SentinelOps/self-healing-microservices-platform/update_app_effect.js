const fs = require('fs');
let content = fs.readFileSync('storefront/src/App.jsx', 'utf8');

const oldEffect = `  useEffect(() => {
    let interval;
    if (status === 'healing') {
      interval = setInterval(async () => {
        try {
          const res = await fetch("http://localhost:8002/health");
          if (res.ok) {
            setStatus('healed');
            clearInterval(interval);
          }
        } catch (e) {}
      }, 2000);
    }
    return () => clearInterval(interval);
  }, [status]);`;

const newEffect = `  useEffect(() => {
    let interval;
    let startTime = Date.now();
    if (status === 'healing') {
      interval = setInterval(async () => {
        if (Date.now() - startTime > 60000) {
          setStatus('escalated');
          clearInterval(interval);
          return;
        }
        let healthOk = false;
        try {
          const res = await fetch("http://localhost:8002/health");
          healthOk = res.ok;
        } catch (e) {}
        
        try {
          const iRes = await fetch("http://localhost:8000/api/incidents");
          if (iRes.ok) {
            const data = await iRes.json();
            const inc = data.find(i => i.id > lastIncidentId && i.service === 'payment-service');
            if (inc) {
              if (inc.status === 'escalated_to_human') {
                 setStatus('escalated');
                 clearInterval(interval);
              } else if (inc.status === 'resolved') {
                 setStatus('healed');
                 clearInterval(interval);
              }
            }
          } else {
            if (healthOk) { setStatus('healed'); clearInterval(interval); }
          }
        } catch(e) {
          if (healthOk) { setStatus('healed'); clearInterval(interval); }
        }
      }, 1000);
    }
    return () => clearInterval(interval);
  }, [status, lastIncidentId]);`;

content = content.replace(oldEffect, newEffect);

fs.writeFileSync('storefront/src/App.jsx', content);
