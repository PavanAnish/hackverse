import { useEffect, useState } from 'react';
import { AlertCircle, CheckCircle2, Clock, Activity, Server, FileText, Bot, Wrench, ShieldCheck, XCircle } from 'lucide-react';

const API_BASE = 'http://localhost:8000/api';

function App() {
  const [incidents, setIncidents] = useState([]);
  const [health, setHealth] = useState({});
  const [selectedIncidentId, setSelectedIncidentId] = useState(null);
  const [pendingAction, setPendingAction] = useState(null);
  const [confirmText, setConfirmText] = useState('');
  const [ack1, setAck1] = useState(false);
  const [ack2, setAck2] = useState(false);
  const [rejectReason, setRejectReason] = useState('');

  const selectedIncident = incidents.find(i => i.id === selectedIncidentId);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [incidentsRes, healthRes] = await Promise.all([
          fetch(`${API_BASE}/incidents`),
          fetch(`${API_BASE}/health`)
        ]);
        
        if (incidentsRes.ok) {
          const data = await incidentsRes.json();
          setIncidents(data);
        }
        
        if (healthRes.ok) {
          setHealth(await healthRes.json());
        }
      } catch (e) {
        console.error("Failed to fetch data", e);
      }
    };

    fetchData();
    const interval = setInterval(fetchData, 2000);
    return () => clearInterval(interval);
  }, []);

  const executePendingAction = async (id) => {
    const endpoint = pendingAction === 'approve' ? `/api/incidents/${id}/approve` : `/api/incidents/${id}/reject`;
    await fetch(`http://localhost:8000${endpoint}`, { method: 'POST' });
    setPendingAction(null);
    setConfirmText('');
  };

  const StatusDot = ({ status }) => {
    switch(status) {
      case 'resolved': return <div className="w-2 h-2 rounded-full bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.5)]" />;
      case 'escalated_to_human': return <div className="w-2 h-2 rounded-full bg-rose-500 shadow-[0_0_8px_rgba(244,63,94,0.5)]" />;
      case 'pending_approval': return <div className="w-2 h-2 rounded-full bg-fuchsia-500 shadow-[0_0_8px_rgba(217,70,239,0.5)] animate-pulse" />;
      default: return <div className="w-2 h-2 rounded-full bg-amber-500 shadow-[0_0_8px_rgba(245,158,11,0.5)] animate-pulse" />;
    }
  };

  const HealthDot = ({ state }) => {
    switch(state) {
      case 'healthy': return <div className="w-2 h-2 rounded-full bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.5)]" />;
      case 'degraded': return <div className="w-2 h-2 rounded-full bg-amber-500 shadow-[0_0_8px_rgba(245,158,11,0.5)] animate-pulse" />;
      case 'down': return <div className="w-2 h-2 rounded-full bg-rose-500 shadow-[0_0_8px_rgba(244,63,94,0.5)]" />;
      default: return <div className="w-2 h-2 rounded-full bg-slate-600" />;
    }
  };

  return (
    <div className="min-h-screen bg-slate-950 text-slate-300 font-sans selection:bg-emerald-500/30">
      <header className="border-b border-slate-800/50 bg-slate-950/80 backdrop-blur sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-4 h-16 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <ShieldCheck className="w-6 h-6 text-emerald-500" />
            <h1 className="text-xl font-semibold text-slate-100 tracking-tight font-heading">SentinelOps <span className="text-slate-600 font-normal">| Live Dashboard</span></h1>
          </div>
          
          <div className="flex items-center gap-8 px-2">
            {['order-service', 'payment-service', 'inventory-service'].map(svc => (
              <div key={svc} className="flex items-center gap-2.5 text-sm font-medium">
                <HealthDot state={health[svc]} />
                <span className="text-slate-300 font-heading tracking-wide">{svc.replace('-service', '')}</span>
              </div>
            ))}
          </div>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-4 py-8 h-[calc(100vh-4rem)]">
        {incidents.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center text-center animate-in fade-in duration-700">
            <div className="relative">
              <div className="absolute inset-0 bg-emerald-500/20 rounded-full blur-xl animate-pulse"></div>
              <div className="relative w-24 h-24 bg-slate-900 border border-slate-800 rounded-full flex items-center justify-center mb-6 shadow-2xl">
                <ShieldCheck className="w-10 h-10 text-emerald-500" />
              </div>
            </div>
            <h2 className="text-2xl font-semibold text-slate-100 font-heading mb-3 tracking-tight">All Systems Nominal</h2>
            <p className="text-slate-400 max-w-md text-sm leading-relaxed">
              No active incidents detected. The SentinelOps AI agent is actively monitoring the event stream across all services.
            </p>
          </div>
        ) : (
          <div className="grid grid-cols-12 gap-8 h-full">
            {/* Left Col: Feed */}
            <div className="col-span-4 flex flex-col gap-6">
              <div className="flex items-center justify-between pb-4 border-b border-slate-800/50">
                <h2 className="text-xl font-semibold text-slate-200 flex items-center gap-2 font-heading">
                  <Activity className="w-5 h-5 text-emerald-500" /> Incident Feed
                </h2>
                <div className="flex items-center gap-2">
                  <span className="relative flex h-2 w-2">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                  </span>
                  <span className="text-xs text-slate-500 font-mono uppercase tracking-wider">Live</span>
                </div>
              </div>
              
              <div className="flex-1 overflow-y-auto space-y-1 pr-2 custom-scrollbar pb-10">
                {incidents.map(inc => (
                  <div 
                    key={inc.id}
                    onClick={() => { setSelectedIncidentId(inc.id); setPendingAction(null); setConfirmText(''); setAck1(false); setAck2(false); }}
                    className={`p-4 transition-all cursor-pointer border-b border-slate-800/30 group
                      ${selectedIncident?.id === inc.id ? 'bg-slate-900/50 rounded-lg shadow-sm border-b-transparent' : 'hover:bg-slate-900/30 rounded-lg'}
                      ${inc.status === 'escalated_to_human' ? 'border-l-rose-500 border-l-4' : ''}
                      ${inc.status === 'resolved' ? 'border-l-emerald-500 border-l-4' : ''}
                      ${inc.status === 'in_progress' ? 'border-l-amber-500 border-l-4' : ''}
                      ${inc.status === 'pending_approval' ? 'border-l-fuchsia-500 border-l-4' : ''}
                      ${!['escalated_to_human', 'resolved', 'in_progress', 'pending_approval'].includes(inc.status) ? 'border-l-transparent border-l-4' : ''}
                    `}
                  >
                    <div className="flex justify-between items-center mb-2">
                      <div className="flex items-center gap-2">
                        <StatusDot status={inc.status} />
                        <span className={`font-medium capitalize tracking-wide font-heading ${selectedIncident?.id === inc.id ? 'text-slate-100' : 'text-slate-300 group-hover:text-slate-200'}`}>
                          {inc.service}
                        </span>
                      </div>
                      <span className="text-xs font-mono text-slate-500">
                        {new Date(inc.created_at).toLocaleTimeString([], {hour12:false})}
                      </span>
                    </div>
                    <p className="text-sm text-slate-400/90 line-clamp-2 leading-relaxed ml-4">{inc.event_msg}</p>
                  </div>
                ))}
              </div>
            </div>

            {/* Right Col: Details */}
            <div className="col-span-8 bg-slate-900/20 rounded-2xl flex flex-col border border-slate-800/30 shadow-2xl overflow-hidden min-h-0">
              {selectedIncident ? (
                <div className="flex-1 overflow-y-auto p-8 custom-scrollbar">
                  
                  <div className="flex justify-between items-end border-b border-slate-800/50 pb-6 mb-8">
                    <div>
                      <h2 className="text-3xl font-bold text-slate-100 flex items-center gap-3 mb-3 font-heading">
                        {selectedIncident.service} Incident
                      </h2>
                      <div className="flex items-center gap-4 text-sm font-heading tracking-wide">
                        <div className="flex items-center gap-2">
                          <StatusDot status={selectedIncident.status} />
                          <span className="text-slate-300 capitalize">{selectedIncident.status.replace('_', ' ')}</span>
                        </div>
                        <span className="text-slate-600 font-mono">ID: {selectedIncident.id}</span>
                      </div>
                    </div>
                    <div className="text-right flex flex-col items-end">
                      <div className="text-xs text-slate-500 uppercase tracking-widest font-semibold mb-1">Time to Resolution</div>
                      <div className="font-mono text-slate-300 text-lg">
                        {selectedIncident.resolved_at 
                          ? `${((new Date(selectedIncident.resolved_at) - new Date(selectedIncident.created_at)) / 1000).toFixed(1)}s`
                          : 'Pending...'}
                      </div>
                    </div>
                  </div>

                  <div className="grid grid-cols-2 gap-10">
                    <div className="space-y-4">
                      <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-widest font-heading">Trigger Event</h3>
                      <div className="font-mono text-sm text-amber-200/80 break-words leading-relaxed pl-4 border-l border-slate-800">
                        {selectedIncident.event_msg}
                      </div>
                    </div>
                    <div className="space-y-4">
                      <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-widest font-heading">Runbook Context</h3>
                      <div className="font-mono text-sm text-slate-400 whitespace-pre-wrap h-32 overflow-y-auto leading-relaxed pl-4 border-l border-slate-800 custom-scrollbar">
                        {selectedIncident.runbook_chunk || 'No runbook retrieved.'}
                      </div>
                    </div>
                  </div>

                  {selectedIncident.root_cause && (
                    <div className="pt-8 border-t border-slate-800/50 mt-8">
                      <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-widest mb-6 font-heading flex items-center gap-2">
                        <Bot className="w-4 h-4" /> LLM Analysis & Confidence
                      </h3>
                      <div className="grid grid-cols-12 gap-8">
                        <div className="col-span-8">
                          <div className="text-slate-300 text-lg leading-relaxed font-heading font-light">{selectedIncident.root_cause}</div>
                        </div>
                        <div className="col-span-4">
                          <div className="flex items-center gap-4 mt-2">
                            <div className="h-1 flex-1 bg-slate-800 rounded-full overflow-hidden">
                              <div 
                                className={`h-full rounded-full ${selectedIncident.confidence >= 0.8 ? 'bg-emerald-500' : selectedIncident.confidence >= 0.5 ? 'bg-amber-500' : 'bg-rose-500'}`}
                                style={{ width: `${(selectedIncident.confidence || 0) * 100}%` }}
                              />
                            </div>
                            <span className="font-mono text-lg text-slate-300">{Math.round((selectedIncident.confidence || 0) * 100)}%</span>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {selectedIncident.action_taken && (
                    <div className="pt-8 border-t border-slate-800/50 mt-8">
                      <div className="flex justify-between items-center mb-4">
                        <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-widest font-heading flex items-center gap-2">
                          <Wrench className="w-4 h-4" /> Action Taken
                        </h3>
                        <div className="flex gap-3">
                          {selectedIncident.status === 'escalated_to_human' && (
                            <button 
                              onClick={async () => {
                                await fetch('http://localhost:8002/chaos/fail?enable=false', {method: 'POST'});
                                alert("System restored! In a real scenario, this would apply your manual fix.");
                              }}
                              className="bg-rose-500 hover:bg-rose-600 text-white px-4 py-2 rounded font-bold text-sm transition-colors shadow-lg shadow-rose-500/20"
                            >
                              Manual Override: Restore System
                            </button>
                          )}
                          {selectedIncident.status === 'pending_approval' && !pendingAction && (
                            <>
                              <button 
                                onClick={() => { setPendingAction('approve'); setConfirmText(''); }}
                                className="bg-emerald-600 hover:bg-emerald-700 text-white px-4 py-2 rounded font-bold text-sm transition-colors shadow-lg shadow-emerald-500/20"
                              >
                                Approve
                              </button>
                              <button 
                                onClick={() => { setPendingAction('reject'); setConfirmText(''); }}
                                className="bg-slate-700 hover:bg-rose-600 text-white px-4 py-2 rounded font-bold text-sm transition-colors shadow-lg shadow-slate-900/50"
                              >
                                Reject
                              </button>
                            </>
                          )}
                        </div>
                      </div>
                      {selectedIncident.status === 'pending_approval' ? (
                        <div className="font-mono text-emerald-400 text-base mb-4">
                          &gt; {selectedIncident.action_taken}
                        </div>
                      ) : (
                        <div className="font-mono text-sm bg-slate-900/50 p-5 rounded-lg border border-slate-800 space-y-3 mb-4 shadow-inner">
                          <div className="flex gap-4">
                            <span className="text-slate-500 w-32 uppercase tracking-wider text-xs font-semibold">Action:</span>
                            <span className="text-emerald-400 font-bold">{selectedIncident.action_taken}</span>
                          </div>
                          <div className="flex gap-4">
                            <span className="text-slate-500 w-32 uppercase tracking-wider text-xs font-semibold">Service:</span>
                            <span className="text-slate-300">{selectedIncident.service}</span>
                          </div>
                          {selectedIncident.expected_effect && (
                            <div className="flex gap-4">
                              <span className="text-slate-500 w-32 uppercase tracking-wider text-xs font-semibold">Expected Effect:</span>
                              <span className="text-slate-300">{selectedIncident.expected_effect}</span>
                            </div>
                          )}
                          {selectedIncident.resolved_at && (
                            <div className="flex gap-4">
                              <span className="text-slate-500 w-32 uppercase tracking-wider text-xs font-semibold">Executed At:</span>
                              <span className="text-slate-400">{new Date(selectedIncident.resolved_at).toLocaleString()}</span>
                            </div>
                          )}
                        </div>
                      )}

                      {pendingAction && selectedIncident.status === 'pending_approval' && (
                        <div className="mt-6 p-6 bg-slate-950/90 rounded-xl border border-slate-700 shadow-2xl">
                          <h4 className={`font-bold mb-4 font-heading text-lg flex items-center gap-2 ${pendingAction === 'approve' ? 'text-emerald-400' : 'text-rose-400'}`}>
                            {pendingAction === 'approve' ? 'Review & Approve Change' : 'Confirm Rejection'}
                          </h4>
                          
                          {pendingAction === 'approve' ? (
                            <>
                              <div className="bg-slate-900 p-5 rounded-lg border border-slate-800 mb-6 font-mono text-sm space-y-3 text-slate-300">
                                <div className="border-b border-slate-800 pb-3 mb-3 font-bold text-slate-100 uppercase tracking-wider text-xs flex items-center gap-2">
                                  <ShieldCheck className="w-4 h-4 text-emerald-500" /> Proposed Change
                                </div>
                                <div className="grid grid-cols-[140px_1fr] gap-y-3 gap-x-4">
                                  <div className="text-slate-500 uppercase text-xs font-semibold">Service:</div>
                                  <div className="text-emerald-400 font-semibold">{selectedIncident.service}</div>
                                  
                                  <div className="text-slate-500 uppercase text-xs font-semibold">Action:</div>
                                  <div className="text-slate-100">{selectedIncident.action_taken}</div>
                                  
                                  <div className="text-slate-500 uppercase text-xs font-semibold">Expected Effect:</div>
                                  <div className="text-slate-300">{selectedIncident.expected_effect || 'N/A'}</div>
                                  
                                  <div className="text-slate-500 uppercase text-xs font-semibold">Risk Note:</div>
                                  <div className="text-amber-400">{selectedIncident.risk_note || 'N/A'}</div>
                                  
                                  <div className="text-slate-500 uppercase text-xs font-semibold">Reversible:</div>
                                  <div className={selectedIncident.reversible ? 'text-emerald-400' : 'text-rose-400'}>
                                    {selectedIncident.reversible ? 'Yes' : 'No (cache will need to warm up again)'}
                                  </div>
                                </div>
                              </div>
                              
                              <div className="space-y-4 mb-8 pl-1">
                                <label className="flex items-start gap-3 text-sm cursor-pointer hover:text-white transition-colors group">
                                  <div className="pt-1">
                                    <input type="checkbox" className="w-4 h-4 accent-emerald-500" checked={ack1} onChange={e => setAck1(e.target.checked)} />
                                  </div>
                                  <span className="text-slate-300 group-hover:text-slate-100">
                                    I understand this will <strong className="text-emerald-400">{selectedIncident.action_taken}</strong> on <strong className="text-emerald-400">{selectedIncident.service}</strong>
                                  </span>
                                </label>
                                <label className="flex items-start gap-3 text-sm cursor-pointer hover:text-white transition-colors group">
                                  <div className="pt-1">
                                    <input type="checkbox" className="w-4 h-4 accent-emerald-500" checked={ack2} onChange={e => setAck2(e.target.checked)} />
                                  </div>
                                  <span className="text-slate-300 group-hover:text-slate-100">
                                    I acknowledge this action <strong className={selectedIncident.reversible ? 'text-emerald-400' : 'text-rose-400'}>
                                      {selectedIncident.reversible ? 'can be undone' : 'cannot be undone this session'}
                                    </strong>
                                  </span>
                                </label>
                              </div>
                            </>
                          ) : (
                            <div className="mb-6">
                              <p className="text-sm text-slate-400 mb-4 leading-relaxed">
                                You are overriding the AI's recommendation to <strong className="text-rose-400">{selectedIncident.action_taken}</strong> for <strong className="text-slate-300">{selectedIncident.root_cause}</strong>.
                              </p>
                              <textarea 
                                className="bg-slate-900 border border-slate-700 p-3 rounded-lg w-full text-white min-h-[100px] outline-none focus:border-slate-500 font-sans text-sm resize-none" 
                                placeholder="Reason for rejection..."
                                value={rejectReason}
                                onChange={(e) => setRejectReason(e.target.value)}
                              />
                            </div>
                          )}

                          <div className="flex gap-4 pt-4 border-t border-slate-800/50">
                            <button 
                              disabled={pendingAction === 'approve' ? !(ack1 && ack2) : rejectReason.trim().length === 0}
                              onClick={() => executePendingAction(selectedIncident.id)}
                              className={`px-8 py-2.5 rounded font-bold transition-all shadow-lg ${
                                (pendingAction === 'approve' ? !(ack1 && ack2) : rejectReason.trim().length === 0)
                                  ? 'bg-slate-800 text-slate-500 cursor-not-allowed shadow-none'
                                  : pendingAction === 'approve' 
                                    ? 'bg-emerald-600 hover:bg-emerald-500 text-white shadow-emerald-500/20' 
                                    : 'bg-rose-600 hover:bg-rose-500 text-white shadow-rose-500/20'
                              }`}
                            >
                              {pendingAction === 'approve' ? 'Execute Change' : 'Reject Change'}
                            </button>
                            <button onClick={() => { setPendingAction(null); setAck1(false); setAck2(false); setRejectReason(''); }} className="px-6 py-2.5 rounded font-medium text-slate-400 hover:text-white transition-colors border border-slate-700 hover:bg-slate-800">
                              Cancel
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                  )}

                  {selectedIncident.post_mortem && (
                    <div className="pt-8 border-t border-slate-800/50 mt-8">
                      <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-widest mb-4 font-heading flex items-center gap-2">
                        <Server className="w-4 h-4" /> Auto-Generated Post Mortem
                      </h3>
                      <div className="font-mono text-sm text-slate-400 whitespace-pre-wrap leading-relaxed pl-4 border-l border-slate-800">
                        {selectedIncident.post_mortem}
                      </div>
                    </div>
                  )}

                </div>
              ) : (
                <div className="flex-1 flex flex-col items-center justify-center text-slate-500 h-full p-10 text-center">
                  <ShieldCheck className="w-16 h-16 text-slate-800 mb-6" />
                  <p className="text-xl font-medium text-slate-400 font-heading">Select an incident to view details</p>
                  <p className="text-sm mt-3 max-w-sm font-heading font-light">The reasoning trace, retrieved runbook chunks, and auto-generated post-mortem will appear here.</p>
                </div>
              )}
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

export default App;
