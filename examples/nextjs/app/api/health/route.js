import { NextResponse } from 'next/server';

/**
 * GET /api/health — liveness probe (public).
 */
export function GET() {
  return NextResponse.json({ status: 'ok' });
}
