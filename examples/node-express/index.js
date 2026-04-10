'use strict';

const express = require('express');

const app = express();
const PORT = process.env.PORT || 3000;

// GET /health — liveness probe (public)
app.get('/health', (_req, res) => {
  res.json({ status: 'ok' });
});

// GET /public — public endpoint, no auth required
app.get('/public', (_req, res) => {
  res.json({
    message: 'This is a public endpoint',
    timestamp: new Date().toISOString(),
  });
});

// GET /protected — protected endpoint
// VibeWarden verifies the request before it reaches here.
// User identity is forwarded via X-User-* headers.
app.get('/protected', (req, res) => {
  const userId = req.headers['x-user-id'] || 'unknown';
  const userEmail = req.headers['x-user-email'] || 'unknown';

  res.json({
    message: 'You reached a protected endpoint',
    user_id: userId,
    user_email: userEmail,
  });
});

app.listen(PORT, '0.0.0.0', () => {
  console.log(`node-express example listening on :${PORT}`);
});
