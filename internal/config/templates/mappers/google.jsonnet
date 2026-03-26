// google.jsonnet — Kratos OIDC claims mapper for Google.
// Maps Google's userinfo claims to VibeWarden identity traits.
local claims = std.extVar('claims');

{
  identity: {
    traits: {
      email: claims.email,
      [if 'name' in claims then 'name']: claims.name,
      [if 'picture' in claims then 'picture']: claims.picture,
    },
  },
}
