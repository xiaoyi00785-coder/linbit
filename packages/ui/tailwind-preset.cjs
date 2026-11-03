/**
 * Linbit Tailwind preset.
 * 所有颜色都映射到 tokens.css 中的 CSS 变量（蓝色系品牌色、深色底），
 * 这样换肤 / 主题切换只需改变量，无需改类名。
 * @type {import('tailwindcss').Config}
 */
module.exports = {
  theme: {
    extend: {
      colors: {
        lb: {
          bg: "var(--lb-bg)",
          "bg-deep": "var(--lb-bg-deep)",
          card: "var(--lb-card)",
          "card-2": "var(--lb-card-2)",
          border: "var(--lb-border)",
          "border-2": "var(--lb-border-2)",
          primary: "var(--lb-primary)",
          "primary-dark": "var(--lb-primary-dark)",
          "on-primary": "var(--lb-on-primary)",
          text: "var(--lb-text)",
          "text-2": "var(--lb-text-2)",
          "text-3": "var(--lb-text-3)",
          up: "var(--lb-up)",
          down: "var(--lb-down)",
          warn: "var(--lb-warn)",
        },
      },
      borderRadius: {
        lb: "12px",
      },
      fontFamily: {
        sans: [
          "-apple-system",
          "BlinkMacSystemFont",
          "Segoe UI",
          "Roboto",
          "PingFang SC",
          "Microsoft YaHei",
          "sans-serif",
        ],
      },
    },
  },
};
