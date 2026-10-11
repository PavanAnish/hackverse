/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        'brand-cream': '#FBF3E7',
        'brand-peach': '#F4D8B8',
        'brand-orange': '#E17C3C',
        'brand-brown': '#4A2F23',
        'brand-brown-light': '#7A5638',
        'brand-gold': '#C69B4E',
        'text-dark': '#241C15',
        'text-muted': '#8A7B6E',
      },
      fontFamily: {
        'display': ['Fraunces', 'Georgia', 'serif'],
        'jakarta': ['"Plus Jakarta Sans"', 'system-ui', 'sans-serif'],
      },
      boxShadow: {
        'soft': '0 8px 30px rgba(74,47,35,0.06)',
        'softer': '0 20px 50px rgba(74,47,35,0.14)',
      },
    },
  },
  plugins: [],
}
