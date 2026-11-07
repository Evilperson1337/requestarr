// What the server's session answer says a role may do (internal/domain/role.go).
// Fixtures for /auth/me use it so the web app is fed capabilities, not role names.
// The values are written out per role on purpose: a fixture that recomputed the
// rule would agree with the app by construction.
const capabilities: Record<string, { request: boolean; manageRequests: boolean; manageSystem: boolean; manageAccess: boolean }> = {
  viewer: { request: false, manageRequests: false, manageSystem: false, manageAccess: false },
  requester: { request: true, manageRequests: false, manageSystem: false, manageAccess: false },
  admin: { request: true, manageRequests: true, manageSystem: true, manageAccess: false },
  superuser: { request: true, manageRequests: true, manageSystem: true, manageAccess: true },
}

export function capabilitiesFor(role: string) {
  const found = capabilities[role]
  if (!found) throw new Error(`no capabilities fixture for role ${role}`)
  return found
}

// The /auth/me answer for a signed-in person of the given role.
export function sessionFixture(role: string, overrides: { id?: string; displayName?: string; email?: string } = {}) {
  return {
    user: { id: overrides.id ?? 'u1', role, displayName: overrides.displayName ?? 'Person', ...(overrides.email ? { email: overrides.email } : {}) },
    capabilities: capabilitiesFor(role),
    csrfToken: 'csrf',
  }
}
