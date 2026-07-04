import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Use standalone ONLY when NOT deploying on Vercel
  output: process.env.VERCEL ? undefined : "standalone",
  
  async redirects() {
    return [
      {
        source: "/settings/sso",
        destination: "/settings",
        permanent: false,
      },
    ];
  },
};

export default nextConfig;
