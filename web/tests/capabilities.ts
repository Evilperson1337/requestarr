// What the server's session answer says a role may do (internal/domain/role.go).
// Fixtures for /auth/me use it so the web app is fed capabilities, not role names.
export function capabilitiesFor(role: string) {
  return {
    request: role === 'requester' || role === 'admin',
    manageRequests: role === 'admin',
    manageSystem: role === 'admin',
  }
}
