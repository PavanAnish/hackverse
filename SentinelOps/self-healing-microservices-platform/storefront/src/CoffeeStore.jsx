import { useState, useEffect, useRef } from 'react';
import { Loader2, AlertTriangle, ShieldCheck } from 'lucide-react';
import './coffee.css';

// â”€â”€ menu data â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
const MENU = [
  { id:'cappuccino', name:'Cappuccino', category:'Coffee', desc:'Espresso, steamed milk, and a thick layer of microfoam.', img:'/menu/cappuccino.jpg', priceSmall:3.50, priceLarge:4.25, tag:'Bestseller', stockId:3 },
  { id:'latte', name:'CaffÃ¨ Latte', category:'Coffee', desc:'Smooth espresso rounded out with extra steamed milk.', img:'/menu/latte.jpg', priceSmall:3.75, priceLarge:4.50, stockId:1 },
  { id:'americano', name:'Americano', category:'Coffee', desc:'Espresso shots lengthened with hot water for a clean finish.', img:'/menu/americano.jpg', priceSmall:3.25, priceLarge:3.95, stockId:2 },
  { id:'espresso', name:'Espresso', category:'Coffee', desc:'A concentrated shot pulled fresh, served straight up.', img:'/menu/espresso.jpg', priceSmall:2.75, priceLarge:3.25 },
  { id:'mocha', name:'Mocha', category:'Coffee', desc:'Espresso and steamed milk with real dark chocolate.', img:'/menu/mocha.jpg', priceSmall:4.25, priceLarge:5.00, tag:'New' },
  { id:'flatwhite', name:'Flat White', category:'Coffee', desc:'Ristretto shots under a thin layer of velvety milk.', img:'/menu/americano.jpg', priceSmall:3.90, priceLarge:4.60 },

  { id:'matcha', name:'Matcha Latte', category:'Non Coffee', desc:'Stone-ground ceremonial matcha whisked with steamed milk.', img:'/menu/matcha.jpg', priceSmall:4.00, priceLarge:4.75, tag:'Bestseller' },
  { id:'hotchoc', name:'Hot Chocolate', category:'Non Coffee', desc:'Melted dark chocolate and steamed milk, no shortcuts.', img:'/menu/hotchoc.jpg', priceSmall:3.50, priceLarge:4.20 },
  { id:'chai', name:'Chai Latte', category:'Non Coffee', desc:'Black tea steeped with warm spices and steamed milk.', img:'/menu/chai.jpg', priceSmall:3.75, priceLarge:4.45 },
  { id:'lemonade', name:'Sparkling Lemonade', category:'Non Coffee', desc:'Fresh-squeezed lemon over ice with a splash of soda.', img:'/menu/lemonade.jpg', priceSmall:3.25, priceLarge:3.95 },

  { id:'avocado', name:'Avocado Toast', category:'Food', desc:'Sourdough, smashed avocado, chili flakes, and lime.', img:'/menu/avocado.jpg', priceSmall:6.50, priceLarge:8.00, tag:'Bestseller' },
  { id:'grilledcheese', name:'Grilled Cheese', category:'Food', desc:'Three-cheese blend, pressed golden on sourdough.', img:'/menu/grilledcheese.jpg', priceSmall:5.75, priceLarge:7.25 },
  { id:'bagel', name:'Bagel & Cream Cheese', category:'Food', desc:'Toasted plain bagel with whipped cream cheese.', img:'/menu/bagel.jpg', priceSmall:4.25, priceLarge:5.10 },

  { id:'croissant', name:'Almond Croissant', category:'Snack', desc:'Buttery layers filled and topped with almond cream.', img:'/menu/croissant.jpg', priceSmall:3.95, priceLarge:4.70, tag:'New' },
  { id:'bananabread', name:'Banana Bread', category:'Snack', desc:'Moist and dense, baked fresh every morning.', img:'/menu/bananabread.jpg', priceSmall:3.50, priceLarge:4.20 },
  { id:'granola', name:'Granola Bar', category:'Snack', desc:'Oats, honey, and toasted nuts pressed and cut by hand.', img:'/menu/granola.jpg', priceSmall:2.75, priceLarge:3.30 },

  { id:'tiramisu', name:'Tiramisu', category:'Dessert', desc:'Espresso-soaked ladyfingers layered with mascarpone.', img:'/menu/tiramisu.jpg', priceSmall:5.25, priceLarge:6.30, tag:'Bestseller' },
  { id:'cheesecake', name:'Cheesecake', category:'Dessert', desc:'Classic New York style with a graham cracker base.', img:'/menu/cheesecake.jpg', priceSmall:5.50, priceLarge:6.60 },
  { id:'brownie', name:'Chocolate Brownie', category:'Dessert', desc:'Fudgy dark chocolate brownie, cut thick.', img:'/menu/brownie.jpg', priceSmall:4.75, priceLarge:5.60 },
];

const CATEGORIES = ['Coffee', 'Non Coffee', 'Food', 'Snack', 'Dessert'];
const fmt = n => '$' + n.toFixed(2);

// â”€â”€ Sidebar nav items â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
const CupIcon = () => (
  <svg className="w-6 h-6 text-brand-orange" fill="currentColor" viewBox="0 0 24 24"><path d="M18.5 2h-13C4.12 2 3 3.12 3 4.5V15c0 3.87 3.13 7 7 7h4c3.87 0 7-3.13 7-7v-3h.5c1.38 0 2.5-1.12 2.5-2.5v-5c0-1.38-1.12-2.5-2.5-2.5zm.5 7.5c0 .28-.22.5-.5.5H18V4.5c0-.28.22-.5.5-.5s.5.22.5.5v5zM14 20h-4c-2.76 0-5-2.24-5-5V4.5c0-.28.22-.5.5-.5h11c.28 0 .5.22.5.5V15c0 2.76-2.24 5-5 5z"/></svg>
);

// â”€â”€ Image with fallback â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
function MenuImg({ src, alt, className }) {
  const [err, setErr] = useState(false);
  if (err) return <div className={`img-placeholder ${className}`}>{alt[0]}</div>;
  return <img src={src} alt={alt} className={className} loading="lazy" onError={() => setErr(true)} />;
}

// â”€â”€ Product Card â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
function ProductCard({ item, stock, onAdd }) {
  const [size, setSize] = useState('small');
  const [qty, setQty] = useState(1);
  const price = size === 'small' ? item.priceSmall : item.priceLarge;

  return (
    <article className="card-enter bg-white/90 backdrop-blur-md rounded-[28px] p-5 shadow-soft hover:shadow-softer hover:-translate-y-1 transition-all duration-300 group flex flex-col">
      <div className="relative rounded-2xl aspect-[4/3] mb-5 overflow-hidden bg-brand-peach/30">
        <MenuImg src={item.img} alt={item.name} className="absolute inset-0 w-full h-full object-cover group-hover:scale-105 transition-transform duration-700" />
        {item.tag && <span className="absolute top-3 left-3 bg-brand-brown/90 text-white text-[10px] font-bold px-2.5 py-1 rounded-full tracking-wide uppercase">{item.tag}</span>}
        <div className="absolute top-3 right-3 bg-white/95 backdrop-blur-sm px-3 py-1 rounded-full shadow-sm">
          <span className="text-brand-orange font-bold text-sm">{fmt(price)}</span>
        </div>
        {item.stockId != null && stock[item.stockId] != null && (
          <div className="absolute bottom-3 left-3 bg-white/90 backdrop-blur-sm px-2.5 py-1 rounded-full shadow-sm">
            <span className="text-emerald-600 font-bold text-[10px] uppercase tracking-wide">In Stock: {stock[item.stockId]}</span>
          </div>
        )}
      </div>

      <div className="flex-1">
        <h3 className="text-xl font-extrabold font-display tracking-tight mb-1.5">{item.name}</h3>
        <p className="text-sm text-text-muted mb-6 leading-relaxed">{item.desc}</p>
        <div className="mb-6">
          <p className="text-[10px] font-bold text-text-muted uppercase tracking-widest mb-3">Select size</p>
          <div className="flex gap-2.5">
            {['small', 'large'].map(s => (
              <button key={s} onClick={() => setSize(s)}
                className={`size-btn flex-1 py-3 rounded-xl text-xs font-extrabold capitalize ${size === s ? 'active' : 'bg-brand-cream text-text-muted hover:bg-brand-peach/30'}`}>
                {s}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="flex items-center gap-3 mt-auto">
        <div className="flex items-center bg-brand-cream rounded-xl p-1 border border-orange-100/50">
          <button onClick={() => setQty(q => Math.max(1, q - 1))} className="p-2 text-text-muted hover:text-brand-brown transition-colors" aria-label="Decrease">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path d="M20 12H4" strokeLinecap="round" strokeWidth="2.5"/></svg>
          </button>
          <span className="px-3 font-bold text-sm w-6 text-center">{qty}</span>
          <button onClick={() => setQty(q => Math.min(20, q + 1))} className="p-2 text-text-muted hover:text-brand-brown transition-colors" aria-label="Increase">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path d="M12 4v16m8-8H4" strokeLinecap="round" strokeWidth="2.5"/></svg>
          </button>
        </div>
        <button onClick={() => onAdd(item, size, qty, price)} className="flex-1 brushed-brass text-white py-4 rounded-xl text-xs font-extrabold shadow-lg shadow-brand-brown/20 hover:opacity-95 transition-opacity">
          Add to cart
        </button>
      </div>
    </article>
  );
}

// â”€â”€ Toast â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
function Toast({ msg }) {
  return (
    <div className="purr-toast bg-brand-brown text-white text-sm font-semibold px-5 py-3 rounded-2xl shadow-softer pointer-events-none">
      {msg}
    </div>
  );
}

// â”€â”€ Demo Controls â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
function DemoControls({ activeTab, simulateOutage, demoStatus }) {
  const isCache = activeTab === 'membership';
  return (
    <div className="flex flex-col space-y-2 bg-brand-cream/50 p-4 rounded-2xl border border-brand-gold/30">
      <span className="text-[10px] text-brand-brown-light font-bold uppercase tracking-widest text-center">Demo Controls</span>
      <button 
        onClick={() => simulateOutage(true, isCache ? 'cache' : 'default')}
        className={`flex items-center justify-center px-4 py-3 rounded-xl text-xs font-bold border transition-colors shadow-sm ${
          isCache 
            ? 'bg-purple-50 text-purple-700 border-purple-200 hover:bg-purple-100'
            : 'bg-red-50 text-red-600 border-red-200 hover:bg-red-100'
        }`}
      >
        <AlertTriangle className="w-4 h-4 mr-2" />
        Simulate {isCache ? 'Cache' : 'DB'} Outage
      </button>
      {demoStatus && (
        <div className="mt-2 text-center text-[10px] font-extrabold animate-pulse text-brand-orange">
          {demoStatus}
        </div>
      )}
    </div>
  );
}

// â”€â”€ Membership Page â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
function MembershipPage({ lastIncidentId }) {
  const [status, setStatus] = useState('idle'); 
  const [subscribedPlan, setSubscribedPlan] = useState(null);
  const [incidentText, setIncidentText] = useState("");

  const plans = [
    { name: 'Cat Friend', price: '$9/mo' },
    { name: 'Barista', price: '$19/mo' },
    { name: 'Roaster', price: '$49/mo' }
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
    return () => clearInterval(poll);
  }, [status, lastIncidentId]);

  const handleSubscribe = async (planName) => {
    setSubscribedPlan(planName);
    setStatus('processing');
    try {
      const res = await fetch("http://localhost:8002/pay");
      if (!res.ok) throw new Error("Payment failed");
      setStatus('success');
    } catch(e) {
      setStatus('waiting');
      setIncidentText("");
    }
  };

  return (
    <div>
      <div className="flex items-end gap-3 mb-2 shrink-0">
        <h1 className="text-3xl sm:text-4xl font-extrabold font-display tracking-tight">Purr Club membership</h1>
      </div>
      <p className="text-text-muted text-sm mb-10 shrink-0">Join the club. Get free drinks and support our cats.</p>
      
      {status === 'success' && (
         <div className="bg-emerald-50 text-emerald-800 p-4 rounded-2xl border border-emerald-200 flex items-center gap-3 font-bold text-lg mb-8 shadow-sm">
            <ShieldCheck className="w-6 h-6 text-emerald-600" /> Subscription activated: {subscribedPlan}
         </div>
      )}

      {status === 'waiting' && (
         <div className="space-y-4 mb-8">
           <div className="bg-red-50 text-red-700 p-4 rounded-2xl border border-red-200 flex items-center justify-center gap-2 font-medium shadow-sm">
             <AlertTriangle className="w-5 h-5" /> Payment couldn't be processed. Please try again shortly.
           </div>
           
           {incidentText && (
             <div className="bg-[#FAF5FF] text-[#6B21A8] p-6 rounded-3xl border border-[#E9D5FF] flex flex-col items-center gap-4 shadow-sm">
               <Loader2 className="w-8 h-8 animate-spin text-[#9333EA]" />
               <div className="text-center space-y-2">
                 <p className="font-extrabold text-lg">Approval Required</p>
                 <div className="mt-2 flex flex-col items-center gap-2">
                   <p className="opacity-80 text-sm font-medium">Approve on the dashboard</p>
                   <a href="http://localhost:3001" target="_blank" rel="noopener noreferrer" className="inline-flex items-center bg-[#9333EA] hover:bg-[#7E22CE] text-white px-5 py-2.5 rounded-xl text-sm font-bold mt-2 shadow-md transition-colors">
                      Open Dashboard to Approve
                   </a>
                 </div>
               </div>
             </div>
           )}
         </div>
      )}
      
      {status === 'rejected' && (
         <div className="bg-red-50 text-red-700 p-6 rounded-3xl border border-red-200 flex flex-col items-center gap-3 shadow-sm mb-8">
            <AlertTriangle className="w-8 h-8 text-red-500" />
            <div className="text-center">
              <p className="font-extrabold text-lg">Recovery rejected by administrator</p>
            </div>
         </div>
      )}

      {status === 'recovered' && (
         <div className="bg-emerald-50 text-emerald-800 p-6 rounded-3xl border border-emerald-200 flex flex-col items-center gap-3 shadow-sm mb-8">
            <ShieldCheck className="w-8 h-8 text-emerald-600" />
            <div className="text-center">
              <p className="font-extrabold text-lg">Approved and recovered. You can subscribe now.</p>
            </div>
         </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {plans.map(p => (
          <div key={p.name} className="bg-white/90 backdrop-blur-md p-8 rounded-[28px] border border-orange-100/60 hover:shadow-softer transition-all flex flex-col items-center text-center shadow-soft">
            <h3 className="text-xl font-extrabold font-display tracking-tight mb-2">{p.name}</h3>
            <div className="text-3xl font-extrabold text-brand-orange mb-6">{p.price}</div>
            <button 
              onClick={() => handleSubscribe(p.name)}
              disabled={['processing', 'waiting'].includes(status)}
              className={`w-full py-4 rounded-2xl font-extrabold text-sm shadow-lg transition-opacity disabled:opacity-40
                ${status === 'recovered' && subscribedPlan === p.name ? 'bg-emerald-600 text-white hover:bg-emerald-700 animate-pulse' :
                  'brushed-brass text-white hover:opacity-95'}`}
            >
              {status === 'processing' && subscribedPlan === p.name ? 'Processingâ€¦' :
               status === 'recovered' && subscribedPlan === p.name ? 'Retry Subscribe' : 'Subscribe'}
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

// â”€â”€ Main CoffeeStore â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
export default function CoffeeStore({ status, handleCheckout, stock, cart, setCart, setStatus, simulateOutage, demoStatus, lastIncidentId }) {
  const [category, setCategory] = useState('Coffee');
  const [search, setSearch] = useState('');
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [toasts, setToasts] = useState([]);
  const [sidebarPage, setSidebarPage] = useState('menu'); // 'menu' | 'orders'
  const [orders, setOrders] = useState([]);
  const toastId = useRef(0);

  // Fetch orders when switching to orders page
  useEffect(() => {
    if (sidebarPage === 'orders') {
      fetch("http://localhost:8001/orders")
        .then(r => r.ok ? r.json() : [])
        .then(data => setOrders(Array.isArray(data) ? data : []))
        .catch(() => setOrders([]));
    }
  }, [sidebarPage]);

  // After successful order, refresh orders and stock
  useEffect(() => {
    if (status === 'success') {
      fetch("http://localhost:8001/orders")
        .then(r => r.ok ? r.json() : [])
        .then(data => setOrders(Array.isArray(data) ? data : []))
        .catch(() => {});
    }
  }, [status]);

  const showToast = (msg) => {
    const id = ++toastId.current;
    setToasts(t => [...t, { id, msg }]);
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), 2200);
  };

  const filtered = MENU.filter(p =>
    p.category === category &&
    (!search.trim() || p.name.toLowerCase().includes(search.toLowerCase()) || p.desc.toLowerCase().includes(search.toLowerCase()))
  );

  // â”€â”€ Cart logic (internal shape: {key, menuId, name, size, price, qty, img, stockId?}) â”€â”€
  const addToCart = (item, size, qty, price) => {
    const key = item.id + ':' + size;
    setCart(prev => {
      const existing = prev.find(i => i.key === key);
      if (existing) return prev.map(i => i.key === key ? { ...i, qty: i.qty + qty } : i);
      return [...prev, { key, menuId: item.id, name: item.name, size, price, qty, img: item.img, stockId: item.stockId }];
    });
    if (status === 'success') setStatus('idle');
    showToast(`${item.name} (${size}) added`);
    setDrawerOpen(true);
  };

  const removeFromCart = (key) => setCart(prev => prev.filter(i => i.key !== key));
  const changeQty = (key, delta) => {
    setCart(prev => prev.map(i => {
      if (i.key !== key) return i;
      const nq = i.qty + delta;
      return nq <= 0 ? null : { ...i, qty: nq };
    }).filter(Boolean));
  };

  const cartCount = cart.reduce((s, i) => s + i.qty, 0);
  const subtotal = cart.reduce((s, i) => s + i.qty * i.price, 0);
  const fee = cart.length ? 0.75 : 0;
  const total = subtotal + fee;

  // Build the adapter for the existing checkout handler:
  // it expects cart = [{id, name, price}] and total = number
  const doCheckout = () => {
    // Build legacy-shaped cart items for the existing handler
    const legacyItems = [];
    cart.forEach(ci => {
      for (let i = 0; i < ci.qty; i++) {
        legacyItems.push({ id: ci.stockId || null, name: ci.name, price: ci.price });
      }
    });
    // The handleCheckout in App.jsx uses cart and total from its own state.
    // We need to set the cart in a format App.jsx understands, then call handleCheckout.
    // But handleCheckout reads from its own `cart` and `total` closure.
    // Instead, we call a custom version that takes our items and total.
    handleCheckout(legacyItems, total);
  };

  const onSuccessClose = () => {
    setDrawerOpen(false);
  };

  // Close drawer on success after a beat
  useEffect(() => {
    if (status === 'success') {
      const t = setTimeout(() => {
        setDrawerOpen(false);
        showToast('Order placed â€” thanks for supporting the shelter cats!');
      }, 1500);
      return () => clearTimeout(t);
    }
  }, [status]);

  const isHealing = status === 'healing';
  const isHealed = status === 'healed';
  const isEscalated = status === 'escalated';
  const isError = status === 'error';
  const isProcessing = status === 'processing';

  return (
    <div className="purr-store flex h-full overflow-hidden min-h-screen">

      {/* â”€â”€ SIDEBAR (lg only) â”€â”€ */}
      <aside className="hidden lg:flex w-72 flex-shrink-0 flex-col py-10 px-8 z-20 sidebar-surface">
        <div className="flex items-center gap-3 mb-12">
          <div className="bg-brand-orange/10 p-2.5 rounded-xl"><CupIcon /></div>
          <span className="text-2xl font-extrabold font-display tracking-tight">Purr'Coffee</span>
        </div>

        <nav className="flex-1 space-y-1.5">
          <button onClick={e => { e.preventDefault(); setSidebarPage('menu'); }}
            className={`w-full flex items-center gap-3.5 px-4 py-3.5 rounded-2xl transition-all duration-300 text-left ${sidebarPage === 'menu' ? 'brushed-brass text-white shadow-lg shadow-brand-brown/20' : 'text-brand-brown-light hover:bg-white/60'}`}>
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" strokeLinecap="round" strokeLinejoin="round"/></svg>
            <span className="font-bold text-sm">Menu</span>
          </button>
          <button onClick={e => { e.preventDefault(); setDrawerOpen(true); }}
            className="w-full flex items-center justify-between px-4 py-3.5 text-brand-brown-light hover:bg-white/60 rounded-2xl transition-all duration-300">
            <div className="flex items-center gap-3.5">
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M3 3h2l.4 2M7 13h10l4-8H5.4M7 13L5.4 5M7 13l-2.293 2.293c-.63.63-.184 1.707.707 1.707H17m0 0a2 2 0 100 4 2 2 0 000-4zm-8 2a2 2 0 11-4 0 2 2 0 014 0z" strokeLinecap="round" strokeLinejoin="round"/></svg>
              <span className="font-semibold text-sm">Cart</span>
            </div>
            {cartCount > 0 && <span className="bg-brand-orange text-white text-[10px] font-bold px-2 py-0.5 rounded-full min-w-[18px] text-center">{cartCount}</span>}
          </button>
          <button onClick={e => { e.preventDefault(); setSidebarPage('orders'); }}
            className={`w-full flex items-center justify-between px-4 py-3.5 rounded-2xl transition-all duration-300 text-left ${sidebarPage === 'orders' ? 'brushed-brass text-white shadow-lg shadow-brand-brown/20' : 'text-brand-brown-light hover:bg-white/60'}`}>
            <div className="flex items-center gap-3.5">
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" strokeLinecap="round" strokeLinejoin="round"/></svg>
              <span className="font-semibold text-sm">My orders</span>
            </div>
            {orders.length > 0 && <span className="bg-brand-orange text-white text-[10px] font-bold px-2 py-0.5 rounded-full min-w-[18px] text-center">{orders.length}</span>}
          </button>
          <button onClick={e => { e.preventDefault(); setSidebarPage('membership'); }}
            className={`w-full flex items-center justify-between px-4 py-3.5 rounded-2xl transition-all duration-300 text-left ${sidebarPage === 'membership' ? 'brushed-brass text-white shadow-lg shadow-brand-brown/20' : 'text-brand-brown-light hover:bg-white/60'}`}>
            <div className="flex items-center gap-3.5">
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M21 13.255A23.931 23.931 0 0112 15c-3.183 0-6.22-.62-9-1.745M16 6V4a2 2 0 00-2-2h-4a2 2 0 00-2 2v2m4 6h.01M5 20h14a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" strokeLinecap="round" strokeLinejoin="round"/></svg>
              <span className="font-semibold text-sm">Membership</span>
            </div>
          </button>
          <button onClick={e => e.preventDefault()} className="w-full flex items-center gap-3.5 px-4 py-3.5 text-brand-brown-light hover:bg-white/60 rounded-2xl transition-all duration-300 text-left">
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" strokeLinecap="round" strokeLinejoin="round"/></svg>
            <span className="font-semibold text-sm">History</span>
          </button>
        </nav>

        <div className="mt-auto space-y-1 pt-8 border-t border-brand-gold/20">
          <DemoControls activeTab={sidebarPage} simulateOutage={simulateOutage} demoStatus={demoStatus} />
          <button onClick={e => e.preventDefault()} className="flex items-center gap-3.5 px-4 py-3 text-brand-brown-light hover:text-brand-orange transition-colors mt-4">
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M4.318 6.318a4.5 4.5 0 000 6.364L12 20.364l7.682-7.682a4.5 4.5 0 00-6.364-6.364L12 7.636l-1.318-1.318a4.5 4.5 0 00-6.364 0z" strokeLinecap="round" strokeLinejoin="round"/></svg>
            <span className="font-semibold text-xs uppercase tracking-wider">Donate to shelter</span>
          </button>
          <button onClick={e => e.preventDefault()} className="flex items-center gap-3.5 px-4 py-3 text-brand-brown-light hover:text-brand-orange transition-colors">
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" strokeLinecap="round" strokeLinejoin="round"/></svg>
            <span className="font-semibold text-xs uppercase tracking-wider">Log out</span>
          </button>
        </div>
      </aside>

      {/* â”€â”€ MOBILE DEMO CONTROLS â”€â”€ */}
      <div className="lg:hidden fixed bottom-4 left-4 z-50 bg-white/95 backdrop-blur-md border border-orange-100 p-3 rounded-2xl shadow-xl">
        <DemoControls activeTab={sidebarPage} simulateOutage={simulateOutage} demoStatus={demoStatus} />
      </div>

      {/* â”€â”€ MAIN CONTENT â”€â”€ */}
      <main className="flex-1 min-w-0 flex flex-col p-5 sm:p-8 lg:p-10 overflow-y-auto custom-scrollbar pb-32">

        {/* mobile logo */}
        <div className="flex lg:hidden items-center gap-3 mb-6">
          <div className="bg-brand-orange/10 p-2 rounded-xl"><CupIcon /></div>
          <span className="text-xl font-extrabold font-display tracking-tight">Purr'Coffee</span>
        </div>

        {/* header with search + cart */}
        <header className="flex items-center gap-3 sm:gap-4 mb-8 relative z-10 shrink-0">
          <div className="flex-1 relative min-w-0">
            <div className="absolute inset-y-0 left-0 pl-5 flex items-center pointer-events-none">
              <svg className="h-5 w-5 text-text-muted/50" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" strokeLinecap="round" strokeLinejoin="round"/></svg>
            </div>
            <input value={search} onChange={e => setSearch(e.target.value)}
              className="block w-full pl-12 pr-6 py-4 bg-white/80 backdrop-blur-sm border border-orange-100/60 rounded-2xl text-sm focus:ring-2 focus:ring-brand-orange/30 focus:border-brand-orange outline-none shadow-sm transition-all placeholder:text-text-muted/40"
              placeholder="Search for your favorite brewâ€¦" />
          </div>
          <button onClick={() => setDrawerOpen(true)} className="relative flex-shrink-0 bg-white border border-orange-100/60 text-brand-brown w-[52px] h-[52px] flex items-center justify-center rounded-2xl shadow-sm hover:bg-brand-peach/40 hover:border-brand-orange/40 transition-all" aria-label="Open cart">
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M3 3h2l.4 2M7 13h10l4-8H5.4M7 13L5.4 5M7 13l-2.293 2.293c-.63.63-.184 1.707.707 1.707H17m0 0a2 2 0 100 4 2 2 0 000-4zm-8 2a2 2 0 11-4 0 2 2 0 014 0z" strokeLinecap="round" strokeLinejoin="round"/></svg>
            <span className="absolute -top-2 -right-2 bg-brand-orange text-white text-[10px] font-bold w-5 h-5 rounded-full flex items-center justify-center ring-2 ring-brand-cream">{cartCount}</span>
            {isHealing && <span className="absolute -top-1 -left-1 w-3 h-3 bg-amber-500 rounded-full animate-pulse" />}
          </button>
        </header>

        {/* Mobile nav for orders */}
        <div className="flex lg:hidden gap-2 mb-6">
          <button onClick={() => setSidebarPage('menu')} className={`px-4 py-2 rounded-xl text-sm font-bold ${sidebarPage === 'menu' ? 'brushed-brass text-white' : 'bg-white/80 text-text-muted border border-orange-100/50'}`}>Menu</button>
          <button onClick={() => setSidebarPage('orders')} className={`px-4 py-2 rounded-xl text-sm font-bold ${sidebarPage === 'orders' ? 'brushed-brass text-white' : 'bg-white/80 text-text-muted border border-orange-100/50'}`}>My orders {orders.length > 0 && `(${orders.length})`}</button>
          <button onClick={() => setSidebarPage('membership')} className={`px-4 py-2 rounded-xl text-sm font-bold ${sidebarPage === 'membership' ? 'brushed-brass text-white' : 'bg-white/80 text-text-muted border border-orange-100/50'}`}>Membership</button>
        </div>

        {sidebarPage === 'orders' ? (
          /* â”€â”€ ORDERS PAGE â”€â”€ */
          <div>
            <h1 className="text-3xl sm:text-4xl font-extrabold font-display tracking-tight mb-2">My orders</h1>
            <p className="text-text-muted text-sm mb-10">{orders.length} order{orders.length !== 1 ? 's' : ''} on record.</p>
            {orders.length === 0 ? (
              <p className="text-text-muted text-center py-20">No orders yet. Grab something from the menu!</p>
            ) : (
              <div className="space-y-4">
                {orders.map(o => (
                  <div key={o.id} className="bg-white/90 rounded-2xl p-5 shadow-soft border border-orange-100/40">
                    <div className="flex justify-between items-start mb-2">
                      <span className="font-bold text-sm">Order #{o.id}</span>
                      <span className="text-brand-orange font-extrabold">{fmt(o.total)}</span>
                    </div>
                    <p className="text-xs text-text-muted">{o.items} item{o.items !== 1 ? 's' : ''} Â· {o.timestamp ? new Date(o.timestamp).toLocaleString() : ''}</p>
                  </div>
                ))}
              </div>
            )}
          </div>
        ) : sidebarPage === 'membership' ? (
          /* â”€â”€ MEMBERSHIP PAGE â”€â”€ */
          <MembershipPage lastIncidentId={lastIncidentId} />
        ) : (
          /* â”€â”€ MENU PAGE â”€â”€ */
          <>
            <div className="flex items-end gap-3 mb-2 shrink-0">
              <h1 className="text-3xl sm:text-4xl font-extrabold font-display tracking-tight">Coffee menu</h1>
              <svg className="steam w-6 h-8 mb-1 text-brand-orange/70 hidden sm:block" viewBox="0 0 24 32" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round">
                <path d="M6 30c4-6-4-8 0-14"/><path d="M12 30c4-6-4-8 0-14"/><path d="M18 30c4-6-4-8 0-14"/>
              </svg>
            </div>
            <p className="text-text-muted text-sm mb-10 shrink-0">Freshly brewed, freshly baked â€” every order supports the shelter cats out back.</p>

            {/* category tabs */}
            <div className="flex flex-wrap gap-3 mb-10 shrink-0">
              {CATEGORIES.map(cat => (
                <button key={cat} onClick={() => setCategory(cat)}
                  className={`shrink-0 px-8 sm:px-10 py-3.5 rounded-2xl font-bold whitespace-nowrap transition-all duration-200 ${cat === category ? 'brushed-brass text-white shadow-lg shadow-brand-brown/20' : 'bg-white/80 backdrop-blur-sm text-text-muted border border-orange-100/50 hover:bg-white hover:text-text-dark shadow-sm'}`}>
                  {cat}
                </button>
              ))}
            </div>

            {filtered.length === 0 ? (
              <div className="flex flex-col items-center justify-center text-center py-24 text-text-muted">
                <svg className="w-12 h-12 mb-4 text-brand-orange/40" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="1.5"><path d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" strokeLinecap="round" strokeLinejoin="round"/></svg>
                <p className="font-semibold text-text-dark mb-1">Nothing matches that search</p>
                <p className="text-sm">Try a different word, or clear the search to see the full menu.</p>
              </div>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-7">
                {filtered.map(p => <ProductCard key={p.id} item={p} stock={stock} onAdd={addToCart} />)}
              </div>
            )}
          </>
        )}
      </main>

      {/* â”€â”€ CART SCRIM â”€â”€ */}
      <div onClick={() => setDrawerOpen(false)}
        className={`cart-scrim fixed inset-0 bg-brand-brown/30 backdrop-blur-[2px] z-30 ${drawerOpen ? 'opacity-100' : 'opacity-0 pointer-events-none'}`} />

      {/* â”€â”€ CART DRAWER â”€â”€ */}
      <aside className={`cart-drawer ${drawerOpen ? '' : 'closed'} fixed top-0 right-0 h-screen w-full sm:w-[420px] bg-brand-cream z-40 shadow-softer flex flex-col`}>
        <div className="flex items-center justify-between px-7 py-6 border-b border-orange-100/60">
          <h2 className="text-xl font-extrabold font-display">Your order</h2>
          <button onClick={() => setDrawerOpen(false)} className="p-2 rounded-full hover:bg-white/70 transition-colors" aria-label="Close cart">
            <svg className="w-5 h-5 text-text-muted" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path d="M6 18L18 6M6 6l12 12" strokeLinecap="round" strokeLinejoin="round"/></svg>
          </button>
        </div>

        {cart.length === 0 ? (
          <div className="flex-1 flex flex-col items-center justify-center text-center px-10 text-text-muted">
            <svg className="w-10 h-10 mb-4 text-brand-orange/40" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="1.5"><path d="M3 3h2l.4 2M7 13h10l4-8H5.4M7 13L5.4 5M7 13l-2.293 2.293c-.63.63-.184 1.707.707 1.707H17" strokeLinecap="round" strokeLinejoin="round"/></svg>
            <p className="font-semibold text-text-dark mb-1">Your cart is empty</p>
            <p className="text-sm">Add something from the menu to get started.</p>
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto custom-scrollbar px-7 py-5 space-y-5">
            {cart.map(item => (
              <div key={item.key} className="flex gap-4 items-center">
                <div className="w-16 h-16 rounded-xl overflow-hidden bg-brand-peach/30 flex-shrink-0">
                  <MenuImg src={item.img} alt={item.name} className="w-full h-full object-cover" />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="font-bold text-sm truncate">{item.name}</p>
                  <p className="text-xs text-text-muted capitalize mb-2">{item.size} Â· {fmt(item.price)}</p>
                  <div className="flex items-center gap-2">
                    <div className="flex items-center bg-brand-cream rounded-lg border border-orange-100/50">
                      <button onClick={() => changeQty(item.key, -1)} className="p-1.5 text-text-muted hover:text-brand-brown" aria-label="Decrease">
                        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path d="M20 12H4" strokeLinecap="round" strokeWidth="2.5"/></svg>
                      </button>
                      <span className="px-2 text-xs font-bold">{item.qty}</span>
                      <button onClick={() => changeQty(item.key, 1)} className="p-1.5 text-text-muted hover:text-brand-brown" aria-label="Increase">
                        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path d="M12 4v16m8-8H4" strokeLinecap="round" strokeWidth="2.5"/></svg>
                      </button>
                    </div>
                    <button onClick={() => removeFromCart(item.key)} className="text-xs text-text-muted hover:text-red-500 font-semibold ml-1">Remove</button>
                  </div>
                </div>
                <p className="font-extrabold text-sm text-brand-orange whitespace-nowrap">{fmt(item.price * item.qty)}</p>
              </div>
            ))}
          </div>
        )}

        {/* Healing / healed / escalated banners INSIDE drawer */}
        {isHealing && (
          <div className="mx-7 mb-3 bg-amber-50 border border-amber-200 text-amber-800 p-4 rounded-2xl text-sm flex flex-col items-center gap-2">
            <Loader2 className="w-5 h-5 animate-spin text-amber-600" />
            <p className="font-bold">SentinelOps AI Auto-Healingâ€¦</p>
            <p className="text-xs opacity-80">System sorting out the issue. Please wait.</p>
          </div>
        )}
        {isHealed && (
          <div className="mx-7 mb-3 bg-emerald-50 border border-emerald-200 text-emerald-800 p-4 rounded-2xl text-sm flex flex-col items-center gap-2">
            <ShieldCheck className="w-5 h-5 text-emerald-600" />
            <p className="font-bold">System Automatically Resolved!</p>
            <p className="text-xs opacity-80">You may now place your order.</p>
          </div>
        )}
        {isEscalated && (
          <div className="mx-7 mb-3 bg-red-50 border border-red-200 text-red-700 p-4 rounded-2xl text-sm flex flex-col items-center gap-2">
            <AlertTriangle className="w-5 h-5 text-red-500" />
            <p className="font-bold">Needs human attention</p>
            <p className="text-xs opacity-80">AI auto-healing could not resolve the issue.</p>
          </div>
        )}
        {isError && (
          <div className="mx-7 mb-3 bg-red-50 border border-red-200 text-red-700 p-4 rounded-2xl text-sm flex items-center justify-center gap-2 font-medium">
            <AlertTriangle className="w-4 h-4" /> Payment failed.
          </div>
        )}

        <div className="px-7 py-6 pb-[140px] sm:pb-6 border-t border-orange-100/60 space-y-4">
          <div className="flex items-center justify-between text-sm text-text-muted">
            <span>Subtotal</span><span className="font-bold text-text-dark">{fmt(subtotal)}</span>
          </div>
          <div className="flex items-center justify-between text-sm text-text-muted">
            <span>Service fee</span><span className="font-bold text-text-dark">{fmt(fee)}</span>
          </div>
          <div className="flex items-center justify-between text-base pt-2 border-t border-dashed border-orange-200">
            <span className="font-bold">Total</span><span className="font-extrabold text-brand-orange text-lg">{fmt(total)}</span>
          </div>
          <button onClick={doCheckout}
            disabled={cart.length === 0 || isProcessing || isHealing || isError || isEscalated}
            className={`w-full py-4 rounded-2xl font-extrabold text-sm shadow-lg shadow-brand-brown/20 transition-opacity disabled:opacity-40 ${isHealed ? 'bg-emerald-600 text-white hover:bg-emerald-700 animate-pulse' : 'brushed-brass text-white hover:opacity-95'}`}>
            {isProcessing ? 'Processingâ€¦' : isHealing ? 'AI Auto-Healingâ€¦' : isHealed ? 'Retry order' : 'Place order'}
          </button>
        </div>
      </aside>

      {/* â”€â”€ TOASTS â”€â”€ */}
      <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-[60] flex flex-col items-center gap-2 pointer-events-none">
        {toasts.map(t => <Toast key={t.id} msg={t.msg} />)}
      </div>
    </div>
  );
}

