# MusicBrainz resolver

Implement this package behind `metadata.Resolver`. Keep provider-specific DTOs here and map them into `domain.ResolvedItem` before returning. Enforce outbound HTTP timeouts, response-size limits, and provider-specific rate limits.
