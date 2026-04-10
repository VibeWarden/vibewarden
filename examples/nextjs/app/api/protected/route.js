import { NextResponse } from 'next/server';

/**
 * GET /api/protected — protected endpoint.
 *
 * VibeWarden verifies the request before it reaches here.
 * User identity is forwarded via X-User-* headers.
 */
export function GET(request) {
  const userId = request.headers.get('x-user-id') ?? 'unknown';
  const userEmail = request.headers.get('x-user-email') ?? 'unknown';

  return NextResponse.json({
    message: 'You reached a protected endpoint',
    user_id: userId,
    user_email: userEmail,
  });
}
