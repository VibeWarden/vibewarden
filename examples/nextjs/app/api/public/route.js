import { NextResponse } from 'next/server';

/**
 * GET /api/public — public endpoint, no authentication required.
 */
export function GET() {
  return NextResponse.json({
    message: 'This is a public endpoint',
    timestamp: new Date().toISOString(),
  });
}
