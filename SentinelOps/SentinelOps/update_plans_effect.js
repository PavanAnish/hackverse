const fs = require('fs');
let content = fs.readFileSync('storefront/src/App.jsx', 'utf8');

const oldEffect = `  useEffect(() => {
    let timer, poll;
    if (status === 'waiting') {
      timer = setInterval(() => {
         const el = Math.floor((Date.now() - startRef.current) / 1000);
         if (el >= 180) {
            setStatus('timeout');
            clearInterval(timer);
            clearInterval(poll);
         }
         setTime(el);
      }, 1000);

      poll = setInterval(async () => {
         try {
            const incRes = await fetch("http://localhost:8000/api/incidents");
            if (incRes.ok) {
               const data = await incRes.json();
               if (data.length > 0) {
                  const latest = data[0];
                  const incTime = new Date(latest.created_at).getTime();
                  if (incTime > startRef.current) { 
                     setIncidentText(\`Approval requested (\${latest.status}) at \${Math.floor((Date.now() - startRef.current)/1000)}s\`);
                  }
               }
            }
         } catch(e) {}

         try {
            const hRes = await fetch("http://localhost:8002/health");
            if (hRes.ok) {
               setStatus('recovered');
               clearInterval(poll);
               clearInterval(timer);
            }
         } catch(e) {}
      }, 1000);
    }
    return () => { clearInterval(timer); clearInterval(poll); };
  }, [status]);`;

const newEffect = `  useEffect(() => {
    let timer, poll;
    if (status === 'waiting') {
      timer = setInterval(() => {
         const el = Math.floor((Date.now() - startRef.current) / 1000);
         if (el >= 180) {
            setStatus('timeout');
            clearInterval(timer);
            clearInterval(poll);
         }
         setTime(el);
      }, 1000);

      let approvalSeen = false;
      poll = setInterval(async () => {
         let healthOk = false;
         try {
            const hRes = await fetch("http://localhost:8002/health");
            healthOk = hRes.ok;
         } catch(e) {}
         
         try {
            const incRes = await fetch("http://localhost:8000/api/incidents");
            if (incRes.ok) {
               const data = await incRes.json();
               const inc = data.find(i => i.id > lastIncidentId && i.service === 'payment-service');
               if (inc) {
                  if (inc.status === 'pending_approval') {
                     setIncidentText("Approval Required");
                     approvalSeen = true;
                  } else if (inc.status === 'resolved' && healthOk) {
                     setStatus('recovered');
                     clearInterval(poll);
                     clearInterval(timer);
                  } else if (inc.status === 'rejected') {
                     setStatus('rejected');
                     clearInterval(poll);
                     clearInterval(timer);
                  }
               }
            } else {
               if (!healthOk && !approvalSeen) setIncidentText("Approval Required fallback");
            }
         } catch(e) {
            if (!healthOk && !approvalSeen) setIncidentText("Approval Required fallback");
         }
      }, 1000);
    }
    return () => { clearInterval(timer); clearInterval(poll); };
  }, [status, lastIncidentId]);`;

content = content.replace(oldEffect, newEffect);
fs.writeFileSync('storefront/src/App.jsx', content);
