# MusicBrainz resolver

Implements `metadata.Resolver`. Albums are identified by release group and songs by recording; a pasted release link resolves to its release group (D020). Provider-specific DTOs stay here and are mapped into `domain.ResolvedItem` before returning. `Aliaser` looks up the release and release-track ids that library Sources expose for a catalog item, spaced to MusicBrainz's one-request-per-second rule. Enforce outbound HTTP timeouts, response-size limits, and provider-specific rate limits.
