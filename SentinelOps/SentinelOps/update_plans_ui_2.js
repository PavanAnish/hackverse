const fs = require('fs');
let content = fs.readFileSync('storefront/src/App.jsx', 'utf8');

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

content = content.replace(oldWaitingUI, newWaitingUI);
fs.writeFileSync('storefront/src/App.jsx', content);
