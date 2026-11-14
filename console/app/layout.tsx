import type { Metadata } from "next";
import { ReactNode } from "react";
import { Shell } from "@/components/shell";
import "./globals.css";

export const metadata: Metadata = {
  title: "Rungated Console",
  description: "Runtime immune system for AI agents",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" data-theme="dark" suppressHydrationWarning>
      <body className="font-sans antialiased">
        <Shell>{children}</Shell>
      </body>
    </html>
  );
}
