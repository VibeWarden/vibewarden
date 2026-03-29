export const metadata = {
  title: 'VibeWarden Next.js Example',
  description: 'Minimal Next.js app demonstrating VibeWarden sidecar integration',
};

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
