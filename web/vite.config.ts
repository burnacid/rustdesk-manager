import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // During `npm run dev`, forward API calls to the Go server.
    proxy: {
      "/admin/api": "http://localhost:21114",
      "/api": "http://localhost:21114",
    },
  },
});
