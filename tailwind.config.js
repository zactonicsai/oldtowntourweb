/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./public/**/*.{html,js}'],
  theme: {
    extend: {
      colors: {
        brand: {
          50:'#F5EFE0',100:'#EBE2CB',200:'#D6C8A6',300:'#B8A57B',400:'#8B6F47',
          500:'#5B6E4C',600:'#3F5A3E',700:'#2E4A34',800:'#223A28',900:'#17291C',
        },
        sepia: { 50:'#FAF5E9',100:'#F0E6CC',600:'#8B6F47',700:'#6B4F2E' }
      },
      fontFamily: {
        serif: ['Georgia','ui-serif','serif'],
        display: ['"Playfair Display"','Georgia','serif'],
      }
    }
  }
}