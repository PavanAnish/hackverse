import os

def write_file(path, content):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, 'w') as f:
        f.write(content)

# 1. package.json
write_file("storefront/package.json", """{
  "name": "storefront",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "lint": "eslint .",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "lucide-react": "^0.344.0"
  },
  "devDependencies": {
    "@vitejs/plugin-react": "^4.3.1",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.47",
    "tailwindcss": "^3.4.13",
    "vite": "^5.4.8"
  }
}
""")

# 2. vite.config.js
write_file("storefront/vite.config.js", """import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
})
""")

# 3. tailwind.config.js
write_file("storefront/tailwind.config.js", """/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {},
  },
  plugins: [],
}
""")

# 4. postcss.config.js
write_file("storefront/postcss.config.js", """export default {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
}
""")

# 5. index.html
write_file("storefront/index.html", """<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Sentinel Storefront</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.jsx"></script>
  </body>
</html>
""")

# 6. src/index.css
write_file("storefront/src/index.css", """@tailwind base;
@tailwind components;
@tailwind utilities;

body {
  @apply bg-gray-900 text-gray-100;
}
""")

# 7. src/main.jsx
write_file("storefront/src/main.jsx", """import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.jsx'
import './index.css'

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
""")

# 8. src/App.jsx
write_file("storefront/src/App.jsx", """import { useState } from 'react';
import { ShoppingCart, Server, Database, Activity, AlertTriangle, ShieldCheck } from 'lucide-react';

const products = [
  { id: 1, name: "Sentinel Pro Server", price: 1299.99, icon: Server },
  { id: 2, name: "Managed DB Cluster", price: 599.99, icon: Database },
  { id: 3, name: "Load Balancer Addon", price: 49.99, icon: Activity }
];

function App() {
  const [cart, setCart] = useState([]);
  const [status, setStatus] = useState('idle');

  const addToCart = (product) => {
    setCart([...cart, product]);
    setStatus('idle');
  };

  const total = cart.reduce((sum, item) => sum + item.price, 0);

  const handleCheckout = async () => {
    if (cart.length === 0) return;
    setStatus('processing');
    try {
      const res = await fetch("http://localhost:8002/pay");
      if (!res.ok) throw new Error("Payment failed");
      setStatus('success');
      setCart([]);
    } catch (e) {
      setStatus('error');
    }
  };

  const simulateOutage = async (enable) => {
    try {
      await fetch(`http://localhost:8002/chaos/fail?enable=${enable}`, { method: 'POST' });
    } catch (e) {
      console.error(e);
    }
  };

  return (
    <div className="min-h-screen bg-gray-900 text-gray-100 p-8 font-sans">
      <header className="flex justify-between items-center mb-12 border-b border-gray-800 pb-4">
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
                  <span className="text-lg font-bold">${p.price}</span>
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
          
          <div className="space-y-4 mb-6 min-h-32">
            {cart.length === 0 ? (
              <p className="text-gray-500 text-center mt-8">Your cart is empty</p>
            ) : (
              cart.map((item, i) => (
                <div key={i} className="flex justify-between items-center text-sm">
                  <span>{item.name}</span>
                  <span className="text-gray-400">${item.price}</span>
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
            onClick={handleCheckout}
            disabled={cart.length === 0 || status === 'processing'}
            className="w-full bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-white py-3 rounded-lg font-bold transition-colors mb-4"
          >
            {status === 'processing' ? 'Processing...' : 'Checkout'}
          </button>

          {status === 'success' && (
            <div className="bg-emerald-900/50 text-emerald-400 p-3 rounded-lg text-center border border-emerald-800">
              Order placed! Payment successful.
            </div>
          )}
          
          {status === 'error' && (
            <div className="bg-red-900/50 text-red-400 p-3 rounded-lg text-center border border-red-800 font-medium">
              Payment failed. Please try again in a moment.
            </div>
          )}
        </div>
      </div>

      {/* Admin Outage Toggle */}
      <div className="fixed bottom-4 right-4 bg-gray-950/80 p-4 rounded-xl border border-gray-800 backdrop-blur-sm">
        <div className="flex flex-col space-y-2">
          <span className="text-xs text-gray-500 font-mono mb-1 uppercase tracking-widest text-center">Demo Controls</span>
          <button 
            onClick={() => simulateOutage(true)}
            className="flex items-center px-4 py-2 bg-red-950 hover:bg-red-900 text-red-500 rounded text-sm border border-red-900/50 transition-colors"
          >
            <AlertTriangle className="w-4 h-4 mr-2" />
            Simulate Outage
          </button>
          <button 
            onClick={() => simulateOutage(false)}
            className="px-4 py-2 bg-gray-800 hover:bg-gray-700 text-gray-400 rounded text-sm transition-colors"
          >
            Restore Systems
          </button>
        </div>
      </div>
    </div>
  );
}

export default App;
""")

# 9. Dockerfile
write_file("storefront/Dockerfile", """FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
""")

print("Storefront scaffolded.")
