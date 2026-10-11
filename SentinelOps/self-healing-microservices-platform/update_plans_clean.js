const fs = require('fs');
let content = fs.readFileSync('storefront/src/App.jsx', 'utf8');

const plansPageStart = content.indexOf('function PlansPage');
const plansPageEnd = content.indexOf('function App()');

let plansPageContent = content.substring(plansPageStart, plansPageEnd);

// 1. Remove time state from PlansPage
plansPageContent = plansPageContent.replace("const [time, setTime] = useState(0);\n", "");
plansPageContent = plansPageContent.replace("const [time, setTime] = useState(0);", "");

// 2. Update useEffect
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

const newEffect = `  useEffect(() => {
    let poll;
    if (status === 'waiting') {
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
                  } else if (inc.status === 'rejected') {
                     setStatus('rejected');
                     clearInterval(poll);
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
    return () => { clearInterval(poll); };
  }, [status, lastIncidentId]);`;

plansPageContent = plansPageContent.replace(oldEffect, newEffect);

// 3. Update UI
const oldWaitingUI = `{status === 'waiting' && (
         <div className="bg-fuchsia-900/40 text-fuchsia-400 p-6 rounded-xl border border-fuchsia-800/50 flex flex-col items-center gap-4">
            <Loader2 className="w-8 h-8 animate-spin text-fuchsia-500" />
            <div className="text-center space-y-2">
              <p className="font-bold text-lg">Problem detected. AI agent is analysing the failure...</p>
              <p className="font-mono text-sm mt-4">Time elapsed: {time}s</p>
              {incidentText && (
                <div className="mt-4 flex flex-col items-center gap-2">
                  <p className="opacity-80">Approve on the dashboard</p>
                  <a href="http://localhost:3001" target="_blank" rel="noopener noreferrer" className="inline-flex items-center bg-blue-600 hover:bg-blue-500 text-white px-4 py-2 rounded text-sm font-medium mt-2">
                     Open Dashboard to Approve
                  </a>
                </div>
              )}
            </div>
         </div>
       )}`;

const newWaitingUI = `{status === 'waiting' && (
         <div className="space-y-4">
           <div className="bg-red-900/40 text-red-400 p-4 rounded-lg text-sm border border-red-800/50 flex items-center justify-center gap-2 font-medium">
             <AlertTriangle className="w-5 h-5" /> Payment couldn't be processed. Please try again shortly.
           </div>
           
           {incidentText && (
             <div className="bg-fuchsia-900/40 text-fuchsia-400 p-6 rounded-xl border border-fuchsia-800/50 flex flex-col items-center gap-4">
               <Loader2 className="w-8 h-8 animate-spin text-fuchsia-500" />
               <div className="text-center space-y-2">
                 <p className="font-bold text-lg">Approval Required</p>
                 <div className="mt-2 flex flex-col items-center gap-2">
                   <p className="opacity-80">Approve on the dashboard</p>
                   <a href="http://localhost:3001" target="_blank" rel="noopener noreferrer" className="inline-flex items-center bg-blue-600 hover:bg-blue-500 text-white px-4 py-2 rounded text-sm font-medium mt-2">
                      Open Dashboard to Approve
                   </a>
                 </div>
               </div>
             </div>
           )}
         </div>
       )}`;

plansPageContent = plansPageContent.replace(oldWaitingUI, newWaitingUI);

content = content.substring(0, plansPageStart) + plansPageContent + content.substring(plansPageEnd);
fs.writeFileSync('storefront/src/App.jsx', content);
