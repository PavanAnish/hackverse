const fs = require('fs');
let content = fs.readFileSync('storefront/src/App.jsx', 'utf8');

const oldWaitingUI = `{status === 'waiting' && (
         <div className="bg-fuchsia-900/40 text-fuchsia-400 p-6 rounded-xl border border-fuchsia-800/50 flex flex-col items-center gap-4">
            <Loader2 className="w-8 h-8 animate-spin text-fuchsia-500" />
            <div className="text-center space-y-2">
              <p className="font-bold text-lg">Payment is paused. A recovery action is waiting for administrator approval.</p>
              <p className="opacity-80">Approve on the dashboard</p>
              <a href="http://localhost:3001" target="_blank" rel="noopener noreferrer" className="inline-flex items-center bg-blue-600 hover:bg-blue-500 text-white px-4 py-2 rounded text-sm font-medium mt-2">
                 Open Dashboard to Approve
              </a>
              <p className="font-mono text-sm mt-4">Time elapsed: {time}s</p>
              {incidentText && <p className="text-xs opacity-70 mt-1">{incidentText}</p>}
            </div>
         </div>
       )}`;

const newWaitingUI = `{status === 'waiting' && (
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
       )}`;

content = content.replace(oldWaitingUI, newWaitingUI);
fs.writeFileSync('storefront/src/App.jsx', content);
