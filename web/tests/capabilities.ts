// What the server's session answer says a role may do (internal/domain/role.go).
// Fixtures for /auth/me use it so the web app is fed capabilities, not role names.
export function capabilitiesFor(role: string) {
  const manages = role === 'admin' || role === 'superuser'
  return {
    request: role === 'requester' || manages,
    manageRequests: manages,
    manageSystem: manages,
  }
}
