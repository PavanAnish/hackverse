const fs = require('fs');
let content = fs.readFileSync('storefront/src/App.jsx', 'utf8');

content = content.replace("function PlansPage({ lastIncidentId }) {\n  const [status, setStatus] = useState('idle');\n  const [lastIncidentId, setLastIncidentId] = useState(0);", "function PlansPage({ lastIncidentId }) {\n  const [status, setStatus] = useState('idle');");

content = content.replace("const [status, setStatus] = useState('idle'); \n  const [demoStatus, setDemoStatus] = useState('');", "const [status, setStatus] = useState('idle');\n  const [lastIncidentId, setLastIncidentId] = useState(0);\n  const [demoStatus, setDemoStatus] = useState('');");

fs.writeFileSync('storefront/src/App.jsx', content);
