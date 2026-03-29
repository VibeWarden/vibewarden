export default function Home() {
  return (
    <main style={{ fontFamily: 'sans-serif', padding: '2rem' }}>
      <h1>VibeWarden — Next.js Example</h1>
      <p>The VibeWarden sidecar is protecting this app.</p>
      <ul>
        <li>
          <a href="/api/health">/api/health</a> — liveness probe (public)
        </li>
        <li>
          <a href="/api/public">/api/public</a> — public endpoint
        </li>
        <li>
          <a href="/api/protected">/api/protected</a> — protected endpoint
        </li>
      </ul>
    </main>
  );
}
