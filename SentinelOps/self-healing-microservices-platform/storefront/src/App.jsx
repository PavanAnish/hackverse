import { useState, useEffect, useRef } from 'react';
import { ShoppingCart, Server, Database, Activity, AlertTriangle, ShieldCheck, Loader2, CheckCircle2 } from 'lucide-react';
import '@fontsource/fraunces';
import '@fontsource/plus-jakarta-sans';
import CoffeeStore from './CoffeeStore';

const products = [
  { id: 1, name: "Sentinel Pro Server", price: 1299.99, icon: Server },
  { id: 2, name: "Managed DB Cluster", price: 599.99, icon: Database },
  { id: 3, name: "Load Balancer Addon", price: 49.99, icon: Activity }
];

function StepRow({ active, text, time, incident, colorClass = "text-emerald-500" }) {
  return (
    <div className={`flex items-start ${active ? 'text-gray-200' : 'text-gray-600'}`}>
       <CheckCircle2 className={`w-5 h-5 mr-3 shrink-0 mt-0.5 ${active ? colorClass : 'text-gray-700'}`} />
       <div className="flex flex-col">
         <div className="flex items-center">
           <span>{text}</span>
           {time !== null && <span className="ml-3 font-mono text-sm">{time}s</span>}
         </div>
         {incident && <span className="text-xs opacity-60 mt-1">({incident})</span>}
       </div>
    </div>
  );
}

function Card1() {
  const [step, setStep] = useState(0); 
  const [time, setTime] = useState(0);
  const [testStatus, setTestStatus] = useState(null);
  const [needsAttn, setNeedsAttn] = useState(false);
  const [incident, setIncident] = useState(null);

  const timerRef = useRef(null);
  const pollRef = useRef(null);
  const stepRef = useRef(0);
  const startRef = useRef(0);

  const reset = () => {
    setStep(0); stepRef.current = 0;
    setTime(0); setTestStatus(null); setNeedsAttn(false); setIncident(null);
    clearInterval(timerRef.current);
    clearInterval(pollRef.current);
  };

  useEffect(() => {
    return () => {
      clearInterval(timerRef.current);
      clearInterval(pollRef.current);
    };
  }, []);

  const handleTest = async () => {
    try {
      const res = await fetch("http://localhost:8002/pay");
      setTestStatus(res.status);
    } catch(e) {
      setTestStatus(500);
    }
  };

  const startSim = async () => {
    reset();
    try {
      await fetch(`http://localhost:8002/chaos/token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enable: true, type: 'default' })
      });
      setStep(1); stepRef.current = 1;
      startRef.current = Date.now();
      
      timerRef.current = setInterval(() => {
        const el = Math.floor((Date.now() - startRef.current) / 1000);
        if (el >= 60 && stepRef.current < 4) {
          setNeedsAttn(true);
          clearInterval(timerRef.current);
          clearInterval(pollRef.current);
        }
        setTime(el);
      }, 1000);

      pollRef.current = setInterval(async () => {
        const curr = stepRef.current;
        if (curr >= 4 || curr === 0) return;

        try {
           const incRes = await fetch("http://localhost:8000/api/incidents");
           if (incRes.ok) {
             const data = await incRes.json();
             if (data.length > 0) {
               setIncident(`seen ${data[0].status} at ${Math.floor((Date.now() - startRef.current)/1000)}s`);
             }
           }
        } catch(e) {}

        try {
          const res = await fetch("http://localhost:8002/health");
          if (!res.ok) {
            if (curr === 1) { setStep(2); stepRef.current = 2; }
            else if (curr === 2) { setStep(3); stepRef.current = 3; }
          } else {
            if (curr >= 2) {
              setStep(4); stepRef.current = 4;
              clearInterval(timerRef.current);
              clearInterval(pollRef.current);
            }
          }
        } catch(e) {
          if (curr === 1) { setStep(2); stepRef.current = 2; }
          else if (curr === 2) { setStep(3); stepRef.current = 3; }
        }
      }, 1000);
    } catch(e) {}
  };

  return (
    <div className="bg-gray-800 p-6 rounded-xl border border-emerald-900/50 flex flex-col">
      <div className="flex justify-between items-center mb-4">
        <h2 className="text-xl font-semibold text-emerald-400">Scenario 1: AI Self-Healing</h2>
        <span className="text-xs font-bold bg-emerald-900/50 text-emerald-400 px-2 py-1 rounded">Autonomous - low risk</span>
      </div>
      <div className="flex gap-4 mb-6 flex-wrap">
        <button onClick={startSim} className="bg-emerald-700 hover:bg-emerald-600 px-4 py-2 rounded text-sm font-medium">Simulate DB Outage</button>
        <button onClick={handleTest} className="bg-gray-700 hover:bg-gray-600 px-4 py-2 rounded text-sm font-medium flex items-center">
          Test Payment {testStatus && <span className="ml-2 bg-gray-900 px-2 py-0.5 rounded text-xs">{testStatus}</span>}
        </button>
        <button onClick={reset} className="ml-auto text-gray-500 hover:text-gray-300 text-sm">Reset card</button>
      </div>
      <div className="mb-6">
        <div className="flex justify-between mb-2">
           {[1,2,3,4].map(num => (
             <div key={num} className={`flex-1 text-center text-xs font-bold ${step >= num ? 'text-emerald-400' : 'text-gray-600'}`}>
               Step {num}
             </div>
           ))}
        </div>
        <div className="flex h-2 bg-gray-900 rounded-full overflow-hidden">
           <div className="bg-emerald-500 transition-all duration-500" style={{width: `${(step/4)*100}%`}}></div>
        </div>
      </div>
      <div className="space-y-4 mb-6 flex-1">
        <StepRow active={step >= 1} text="Failure injected" time={step === 1 ? time : null} />
        <StepRow active={step >= 2} text="Service unhealthy" time={step === 2 ? time : null} incident={incident} />
        <StepRow active={step >= 3} text="AI agent healing" time={step === 3 ? time : null} /> 
        <StepRow active={step >= 4} text="Verified healthy" time={step >= 4 ? time : null} />
        {needsAttn && <p className="text-red-400 text-sm font-bold">Needs human attention</p>}
      </div>
      <div className="bg-gray-900/50 p-4 rounded text-sm border border-gray-700 mt-auto">
        <p><strong>Action:</strong> restart_container on payment-service.</p>
        <p><strong>Effect:</strong> restarts the container and clears the stuck connection pool; /health returns 200 again.</p>
      </div>
    </div>
  )
}

function Card2() {
  const [step, setStep] = useState(0);
  const [time, setTime] = useState(0);
  const [testStatus, setTestStatus] = useState(null);
  const [needsAttn, setNeedsAttn] = useState(false);
  const [incident, setIncident] = useState(null);

  const timerRef = useRef(null);
  const pollRef = useRef(null);
  const stepRef = useRef(0);
  const startRef = useRef(0);

  const reset = () => {
    setStep(0); stepRef.current = 0;
    setTime(0); setTestStatus(null); setNeedsAttn(false); setIncident(null);
    clearInterval(timerRef.current);
    clearInterval(pollRef.current);
  };

  useEffect(() => {
    return () => {
      clearInterval(timerRef.current);
      clearInterval(pollRef.current);
    };
  }, []);

  const handleTest = async () => {
    try {
      const res = await fetch("http://localhost:8002/pay");
      setTestStatus(res.status);
    } catch(e) {
      setTestStatus(500);
    }
  };

  const startSim = async () => {
    reset();
    try {
      await fetch(`http://localhost:8002/chaos/token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enable: true, type: 'cache' })
      });
      setStep(1); stepRef.current = 1;
      startRef.current = Date.now();
      
      timerRef.current = setInterval(() => {
        const el = Math.floor((Date.now() - startRef.current) / 1000);
        if (el >= 180 && stepRef.current < 4) {
          setNeedsAttn(true);
          clearInterval(timerRef.current);
          clearInterval(pollRef.current);
        }
        setTime(el);
      }, 1000);

      pollRef.current = setInterval(async () => {
        const curr = stepRef.current;
        if (curr >= 4 || curr === 0) return;

        try {
           const incRes = await fetch("http://localhost:8000/api/incidents");
           if (incRes.ok) {
             const data = await incRes.json();
             if (data.length > 0) {
               setIncident(`seen ${data[0].status} at ${Math.floor((Date.now() - startRef.current)/1000)}s`);
             }
           }
        } catch(e) {}

        try {
          const res = await fetch("http://localhost:8002/health");
          if (!res.ok) {
            if (curr === 1) { setStep(2); stepRef.current = 2; }
            else if (curr === 2) { setStep(3); stepRef.current = 3; }
          } else {
            if (curr >= 2) {
              setStep(4); stepRef.current = 4;
              clearInterval(timerRef.current);
              clearInterval(pollRef.current);
            }
          }
        } catch(e) {
          if (curr === 1) { setStep(2); stepRef.current = 2; }
          else if (curr === 2) { setStep(3); stepRef.current = 3; }
        }
      }, 1000);
    } catch(e) {}
  };

  return (
    <div className="bg-gray-800 p-6 rounded-xl border border-fuchsia-900/50 flex flex-col">
      <div className="flex justify-between items-center mb-4">
        <h2 className="text-xl font-semibold text-fuchsia-400">Scenario 2: Human Approval</h2>
        <span className="text-xs font-bold bg-fuchsia-900/50 text-fuchsia-400 px-2 py-1 rounded">Human approval required - higher risk</span>
      </div>
      <div className="flex gap-4 mb-6 flex-wrap">
        <button onClick={startSim} className="bg-fuchsia-700 hover:bg-fuchsia-600 px-4 py-2 rounded text-sm font-medium">Simulate Cache Outage</button>
        <button onClick={handleTest} className="bg-gray-700 hover:bg-gray-600 px-4 py-2 rounded text-sm font-medium flex items-center">
          Test Payment {testStatus && <span className="ml-2 bg-gray-900 px-2 py-0.5 rounded text-xs">{testStatus}</span>}
        </button>
        <a href="http://localhost:3001" target="_blank" rel="noopener noreferrer" className="bg-blue-600 hover:bg-blue-500 px-4 py-2 rounded text-sm font-medium flex items-center">
           Open Dashboard to Approve
        </a>
        <button onClick={reset} className="ml-auto text-gray-500 hover:text-gray-300 text-sm">Reset card</button>
      </div>
      <div className="space-y-4 mb-6 flex-1">
        <StepRow active={step >= 1} text="Failure injected" time={step === 1 ? time : null} colorClass="text-fuchsia-500" />
        <StepRow active={step >= 2} text="Service unhealthy" time={step === 2 ? time : null} colorClass="text-fuchsia-500" />
        <StepRow active={step >= 3} text="Waiting for human approval (Approve on the dashboard)" time={step === 3 ? time : null} incident={incident} colorClass="text-fuchsia-500" /> 
        <StepRow active={step >= 4} text="Healthy after approval" time={step >= 4 ? time : null} colorClass="text-fuchsia-500" />
        {needsAttn && <p className="text-red-400 text-sm font-bold">Needs human attention</p>}
      </div>
      <div className="bg-gray-900/50 p-4 rounded text-sm border border-gray-700 mt-auto">
        <p><strong>Action:</strong> clear_cache on payment-service (requires approval).</p>
        <p><strong>Effect:</strong> clears stale cache keys; brief cache-miss latency spike; not reversible.</p>
      </div>
    </div>
  )
}

function PlansPage({ lastIncidentId }) {
  const [status, setStatus] = useState('idle'); 
  const [subscribedPlan, setSubscribedPlan] = useState(null);
  const [incidentText, setIncidentText] = useState("");

  const startRef = useRef(0);

  const plans = [
    { name: 'Starter', price: '$29/mo' },
    { name: 'Pro', price: '$99/mo' },
    { name: 'Enterprise', price: '$299/mo' }
  ];

  useEffect(() => {
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
  }, [status, lastIncidentId]);

  const handleSubscribe = async (planName) => {
    setSubscribedPlan(planName);
    setStatus('processing');
    try {
      const res = await fetch("http://localhost:8002/pay");
      if (!res.ok) throw new Error("Payment failed");
      setStatus('success');
    } catch(e) {
      startRef.current = Date.now();
      setStatus('waiting');
      setIncidentText("");
    }
  };

  return (
    <div className="flex flex-col gap-8 pb-20 p-8 min-h-screen bg-gray-900 text-gray-100">
       <h2 className="text-2xl font-bold text-fuchsia-400 mb-2 mt-12">Upgrade your plan</h2>
       
       {status === 'success' && (
         <div className="bg-emerald-900/50 text-emerald-400 p-4 rounded-xl border border-emerald-800 flex items-center justify-center gap-2 font-bold text-lg">
            <CheckCircle2 className="w-6 h-6" /> Subscription activated: {subscribedPlan}
         </div>
       )}

       {status === 'waiting' && (
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
       )}
       
       {status === 'rejected' && (
         <div className="bg-red-900/40 text-red-400 p-6 rounded-xl border border-red-800/50 flex flex-col items-center gap-3">
            <AlertTriangle className="w-8 h-8 text-red-500" />
            <div className="text-center">
              <p className="font-bold text-lg">Recovery rejected by administrator</p>
            </div>
         </div>
       )}
       
       {status === 'timeout' && (
         <div className="bg-red-900/40 text-red-400 p-6 rounded-xl border border-red-800/50 flex flex-col items-center gap-3">
            <AlertTriangle className="w-8 h-8 text-red-500" />
            <div className="text-center">
              <p className="font-bold text-lg">Needs administrator attention</p>
            </div>
         </div>
       )}

       {status === 'recovered' && (
         <div className="bg-emerald-900/40 text-emerald-400 p-6 rounded-xl border border-emerald-800/50 flex flex-col items-center gap-3">
            <ShieldCheck className="w-8 h-8 text-emerald-500" />
            <div className="text-center">
              <p className="font-bold text-lg">Approved and recovered. You can subscribe now.</p>
            </div>
         </div>
       )}

       {status === 'timeout' && (
         <div className="bg-red-900/40 text-red-400 p-6 rounded-xl border border-red-800/50 flex flex-col items-center gap-3">
            <AlertTriangle className="w-8 h-8 text-red-500" />
            <div className="text-center">
              <p className="font-bold text-lg">Needs administrator attention</p>
            </div>
         </div>
       )}

       <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
         {plans.map(p => (
           <div key={p.name} className="bg-gray-800 p-8 rounded-xl border border-fuchsia-900/30 hover:border-fuchsia-600/50 transition-colors flex flex-col items-center text-center">
             <h3 className="text-xl font-bold mb-2">{p.name}</h3>
             <div className="text-3xl font-extrabold text-fuchsia-400 mb-6">{p.price}</div>
             <button 
               onClick={() => handleSubscribe(p.name)}
               disabled={['processing', 'waiting'].includes(status)}
               className={`w-full py-3 rounded-lg font-bold transition-colors mt-auto text-white
                 ${status === 'waiting' && subscribedPlan === p.name ? 'bg-fuchsia-800 opacity-50 cursor-not-allowed' :
                   status === 'recovered' && subscribedPlan === p.name ? 'bg-emerald-600 hover:bg-emerald-700 animate-pulse' :
                   'bg-fuchsia-600 hover:bg-fuchsia-700 disabled:opacity-80'}`}
             >
               {status === 'processing' && subscribedPlan === p.name ? 'Processing...' :
                status === 'recovered' && subscribedPlan === p.name ? 'Retry Subscribe' : 'Subscribe'}
             </button>
           </div>
         ))}
       </div>
    </div>
  );
}

function App() {
  const [view, setView] = useState('store');
  const isClassic = window.location.search.includes('classic=1');
  const isLegacy = window.location.search.includes('legacy=1');

  const [cart, setCart] = useState([]);
  const [status, setStatus] = useState('idle');
  const [lastIncidentId, setLastIncidentId] = useState(0);
  const [demoStatus, setDemoStatus] = useState('');
  const [stock, setStock] = useState({});

  useEffect(() => {
    const fetchInitial = async () => {
      try {
        const stockRes = await fetch("http://localhost:8003/stock");
        if (stockRes.ok) setStock(await stockRes.json());
      } catch (e) {
        console.error("Failed to load initial data", e);
      }
    };
    fetchInitial();
  }, []);

  const addToCart = (product) => {
    setCart([...cart, product]);
    if (status === 'success') setStatus('idle');
  };

  const total = cart.reduce((sum, item) => sum + item.price, 0);

  const handleCheckout = async (items = cart, t = total) => {
    if (items.length === 0) return;
    setStatus('processing');
    try {
      const res = await fetch("http://localhost:8002/pay");
      if (!res.ok) throw new Error("Payment failed");
      
      try {
        const orderRes = await fetch("http://localhost:8001/orders", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ items, total: t })
        });

        const itemIds = items.map(i => i.id).filter(id => id != null);
        if (itemIds.length > 0) {
          const stockRes = await fetch("http://localhost:8003/stock/decrement", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ item_ids: itemIds })
          });
          if (stockRes.ok) setStock(await stockRes.json());
        }
      } catch(e) {
        console.error("Order or stock update failed", e);
      }

      setStatus('success');
      setCart([]);
    } catch (e) {
      setStatus('error');
      setTimeout(() => setStatus('healing'), 2000);
    }
  };

  useEffect(() => {
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
  }, [status, lastIncidentId]);

  const simulateOutage = async (enable, type = 'default') => {
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
      await fetch(`http://localhost:8002/chaos/token`, { 
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enable, type })
      });
      setDemoStatus(enable ? `Outage Activated! (${type === 'cache' ? 'Cache' : 'DB'} Locked)` : 'Systems Restored');
      setTimeout(() => setDemoStatus(''), 4000);
    } catch (e) {
      console.error(e);
    }
  };

  return (
    <>
      {isClassic && (
        <div className="fixed top-2 left-1/2 -translate-x-1/2 z-[100] flex bg-gray-800/90 backdrop-blur border border-gray-700 p-1 rounded-full shadow-lg">
          <button 
            onClick={() => setView('store')}
            className={`px-4 py-1.5 rounded-full text-sm font-medium transition-colors ${view === 'store' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:text-white'}`}
          >
            Store
          </button>
          <button 
            onClick={() => setView('plans')}
            className={`px-4 py-1.5 rounded-full text-sm font-medium transition-colors ${view === 'plans' ? 'bg-fuchsia-600 text-white' : 'text-gray-300 hover:text-white'}`}
          >
            Plans
          </button>
        </div>
      )}

      {view === 'store' ? (
        !isClassic ? (
          <CoffeeStore 
            status={status}
            setStatus={setStatus}
            handleCheckout={handleCheckout}
            stock={stock}
            cart={cart}
            setCart={setCart}
            simulateOutage={simulateOutage}
            demoStatus={demoStatus}
            lastIncidentId={lastIncidentId}
          />
        ) : (
          <div className="min-h-screen bg-gray-900 text-gray-100 p-8 font-sans">
            <header className="flex flex-col md:flex-row justify-between items-center mb-12 border-b border-gray-800 pb-4 gap-4 mt-8">
              <div className="flex items-center space-x-3">
                <ShieldCheck className="w-8 h-8 text-blue-500" />
                <h1 className="text-2xl font-bold tracking-tight">Sentinel Store</h1>
              </div>
              <div className="flex items-center space-x-2 bg-gray-800 px-4 py-2 rounded-full">
                <ShoppingCart className="w-5 h-5 text-gray-400" />
                <span className="font-semibold">{cart.length} items</span>
              </div>
            </header>
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
              <div className="lg:col-span-2 space-y-6">
                <h2 className="text-xl font-semibold mb-4">Available Products</h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {products.map(p => (
                    <div key={p.id} className="bg-gray-800 p-6 rounded-xl border border-gray-700 hover:border-gray-600 transition-colors">
                      <div className="flex justify-between items-start mb-4">
                        <p.icon className="w-10 h-10 text-blue-400" />
                        <div className="flex flex-col items-end">
                          <span className="text-lg font-bold">${p.price}</span>
                          <span className="text-xs font-medium text-emerald-400 bg-emerald-400/10 px-2 py-0.5 rounded-full mt-1">In Stock: {stock[p.id] !== undefined ? stock[p.id] : '...'}</span>
                        </div>
                      </div>
                      <h3 className="text-lg font-medium mb-4">{p.name}</h3>
                      <button 
                        onClick={() => addToCart(p)}
                        className="w-full bg-blue-600 hover:bg-blue-700 text-white py-2 rounded-lg font-medium transition-colors"
                      >
                        Add to Cart
                      </button>
                    </div>
                  ))}
                </div>
              </div>

              <div className="bg-gray-800 p-6 rounded-xl border border-gray-700 h-fit">
                <h2 className="text-xl font-semibold mb-6 flex items-center">
                  <ShoppingCart className="w-5 h-5 mr-2" />
                  Your Cart
                </h2>
                
                <div className="space-y-4 mb-6 min-h-[120px]">
                  {cart.length === 0 ? (
                    <p className="text-gray-500 text-center mt-8">Your cart is empty</p>
                  ) : (
                    cart.map((item, i) => (
                      <div key={i} className="flex justify-between items-center text-sm bg-gray-900/50 p-3 rounded-lg">
                        <span>{item.name}</span>
                        <span className="text-gray-400">${item.price.toFixed(2)}</span>
                      </div>
                    ))
                  )}
                </div>
                
                <div className="border-t border-gray-700 pt-4 mb-6">
                  <div className="flex justify-between items-center font-bold text-lg">
                    <span>Total:</span>
                    <span>${total.toFixed(2)}</span>
                  </div>
                </div>

                <button 
                  onClick={() => handleCheckout()}
                  disabled={cart.length === 0 || ['processing', 'healing', 'error', 'escalated'].includes(status)}
                  className={`w-full py-3 rounded-lg font-bold transition-colors mb-4 text-white
                    ${status === 'healing' ? 'bg-amber-600 opacity-80' : 
                      status === 'healed' ? 'bg-emerald-600 hover:bg-emerald-700 animate-pulse' :
                      'bg-blue-600 hover:bg-blue-700 disabled:opacity-50'}`}
                >
                  {status === 'processing' ? 'Processing...' : 
                  status === 'healing' ? 'AI Auto-Healing...' :
                  status === 'healed' ? 'Retry Checkout' : 'Checkout'}
                </button>

                {status === 'success' && (
                  <div className="bg-emerald-900/50 text-emerald-400 p-3 rounded-lg text-center border border-emerald-800 flex items-center justify-center gap-2">
                    <CheckCircle2 className="w-5 h-5" /> Order placed successfully!
                  </div>
                )}
                {status === 'error' && (
                  <div className="bg-red-900/50 text-red-400 p-3 rounded-lg text-center border border-red-800 font-medium flex items-center justify-center gap-2">
                    <AlertTriangle className="w-5 h-5" /> Payment failed.
                  </div>
                )}
                {status === 'healing' && (
                  <div className="bg-amber-900/40 text-amber-400 p-4 rounded-lg text-sm border border-amber-800/50 flex flex-col items-center gap-3">
                    <Loader2 className="w-6 h-6 animate-spin text-amber-500" />
                    <div className="text-center">
                      <p className="font-bold mb-1">SentinelOps AI Alerted</p>
                      <p className="opacity-80 text-xs">System sorting out issue automatically. Please wait...</p>
                    </div>
                  </div>
                )}
                {status === 'healed' && (
                  <div className="bg-emerald-900/40 text-emerald-400 p-4 rounded-lg text-sm border border-emerald-800/50 flex flex-col items-center gap-2">
                    <ShieldCheck className="w-6 h-6 text-emerald-500" />
                    <div className="text-center">
                      <p className="font-bold">System Automatically Resolved!</p>
                      <p className="opacity-80 text-xs">The AI agent has restored the service. You may now checkout.</p>
                    </div>
                  </div>
                )}
                {status === 'escalated' && (
                  <div className="bg-red-900/40 text-red-400 p-4 rounded-lg text-sm border border-red-800/50 flex flex-col items-center gap-2">
                    <AlertTriangle className="w-6 h-6 text-red-500" />
                    <div className="text-center">
                      <p className="font-bold">Needs human attention</p>
                      <p className="opacity-80 text-xs">AI auto-healing could not resolve the issue.</p>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        )
      ) : (
        <PlansPage lastIncidentId={lastIncidentId} />
      )}

      {isLegacy && (
        <div className="fixed bottom-4 right-4 bg-gray-950/80 p-4 rounded-xl border border-gray-800 backdrop-blur-sm shadow-2xl z-50">
          <div className="flex flex-col space-y-2 min-w-[160px]">
            <span className="text-xs text-gray-500 font-mono mb-1 uppercase tracking-widest text-center">Demo Controls (Legacy)</span>
            <button 
              onClick={() => simulateOutage(true, 'default')}
              className="flex items-center justify-center px-4 py-2 bg-red-950 hover:bg-red-900 text-red-500 rounded text-sm border border-red-900/50 transition-colors"
            >
              <AlertTriangle className="w-4 h-4 mr-2" />
              Simulate DB Outage
            </button>
            <button 
              onClick={() => simulateOutage(true, 'cache')}
              className="flex items-center justify-center px-4 py-2 bg-fuchsia-950 hover:bg-fuchsia-900 text-fuchsia-500 rounded text-sm border border-fuchsia-900/50 transition-colors"
            >
              <AlertTriangle className="w-4 h-4 mr-2" />
              Simulate Cache Outage
            </button>
            {demoStatus && (
              <div className="mt-2 text-center text-xs font-bold animate-pulse text-amber-500">
                {demoStatus}
              </div>
            )}
          </div>
        </div>
      )}

    </>
  );
}

export default App;
