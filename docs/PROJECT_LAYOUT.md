# Project Layout Rationale

Requestarr uses a modular-monolith layout. `internal/` prevents accidental public Go API commitments while keeping domain and adapters separated. `web/` is independently developable but should eventually be embedded into the release binary or copied into the production image as a version-locked artifact. `api/` is the contract; generated artifacts belong in ignored generated directories. `references/` is never production code.
