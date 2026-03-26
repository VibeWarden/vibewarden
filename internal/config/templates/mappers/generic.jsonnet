// generic.jsonnet — Kratos OIDC claims mapper for generic OIDC providers.
// Used for providers not covered by a dedicated mapper (e.g. custom OIDC).
// Attempts standard OIDC claims: email, name, picture.
local claims = std.extVar('claims');

{
  identity: {
    traits: {
      [if 'email' in claims then 'email']: claims.email,
      [if 'name' in claims then 'name']: claims.name,
      [if 'picture' in claims then 'picture']: claims.picture,
    },
  },
}
