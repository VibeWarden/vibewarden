// github.jsonnet — Kratos OIDC claims mapper for GitHub.
// Maps GitHub's userinfo claims to VibeWarden identity traits.
// GitHub returns the primary email in the `email` claim when the user
// has granted the `user:email` scope, and the display name in `name`.
local claims = std.extVar('claims');

{
  identity: {
    traits: {
      // GitHub may return null for email when the user has set it private.
      [if 'email' in claims && claims.email != null then 'email']: claims.email,
      [if 'name' in claims && claims.name != null then 'name']: claims.name,
      [if 'avatar_url' in claims then 'picture']: claims.avatar_url,
    },
  },
}
