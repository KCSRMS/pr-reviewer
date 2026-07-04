import type { NextConfig } from "next";

const nextConfig: NextConfig = {
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
