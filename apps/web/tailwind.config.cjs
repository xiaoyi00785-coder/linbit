const preset = require("@linbit/ui/tailwind-preset");

/** @type {import('tailwindcss').Config} */
module.exports = {
  presets: [preset],
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}",
    "./legacy/**/*.{ts,tsx}",
    // 扫描 UI 包源码中的类名
    "../../packages/ui/src/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {},
  },
  // 原型样式自带 reset，关闭 preflight 以保持与原型一致
  corePlugins: { preflight: false },
  plugins: [],
};
