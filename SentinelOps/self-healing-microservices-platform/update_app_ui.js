const fs = require('fs');
let content = fs.readFileSync('storefront/src/App.jsx', 'utf8');

content = content.replace(
  "disabled={cart.length === 0 || ['processing', 'healing', 'error'].includes(status)}",
  "disabled={cart.length === 0 || ['processing', 'healing', 'error', 'escalated'].includes(status)}"
);

const healedUI = `{status === 'healed' && (
              <div className="bg-emerald-900/40 text-emerald-400 p-4 rounded-lg text-sm border border-emerald-800/50 flex flex-col items-center gap-2">
                <ShieldCheck className="w-6 h-6 text-emerald-500" />
                <div className="text-center">
                  <p className="font-bold">System Automatically Resolved!</p>
                  <p className="opacity-80 text-xs">The AI agent has restored the service. You may now checkout.</p>
                </div>
              </div>
            )}`;

const escalatedUI = `{status === 'escalated' && (
              <div className="bg-red-900/40 text-red-400 p-4 rounded-lg text-sm border border-red-800/50 flex flex-col items-center gap-2">
                <AlertTriangle className="w-6 h-6 text-red-500" />
                <div className="text-center">
                  <p className="font-bold">Needs human attention</p>
                  <p className="opacity-80 text-xs">AI auto-healing could not resolve the issue.</p>
                </div>
              </div>
            )}`;

content = content.replace(healedUI, healedUI + "\n\n            " + escalatedUI);
fs.writeFileSync('storefront/src/App.jsx', content);
