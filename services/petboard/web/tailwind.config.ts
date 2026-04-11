import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Canvas universe background — a near-black navy.
        space: "#0a0e1a",
      },
    },
  },
  plugins: [],
} satisfies Config;
