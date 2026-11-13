import type { NextConfig } from "next";

// In development the browser talks to Next only, and Next forwards /api to
// the Go server. In production Caddy sends /api straight to the Go server.
const api = process.env.API_URL ?? "http://localhost:8180";

const config: NextConfig = {
  poweredByHeader: false,
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${api}/api/:path*` }];
  },
};

export default config;
