package store

// Keep repository interfaces close to domain use-cases rather than exposing a
// generic ORM. PostgreSQL is the V1 persistence target. SQL should be explicit and
// migrations append-only once merged.
