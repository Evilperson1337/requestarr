package domain

// Role is what a person may do in Requestarr. A first sign-in default is
// never Admin (D035).
type Role string

const (
	RoleViewer    Role = "viewer"
	RoleRequester Role = "requester"
	RoleAdmin     Role = "admin"
)

// ValidDefault reports whether the role can be the default for people who sign
// in for the first time: viewer or requester, never admin.
func (r Role) ValidDefault() bool { return r == RoleViewer || r == RoleRequester }
