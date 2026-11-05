import type { OperationalStatus } from '../api'
import { IntegrationCard } from './IntegrationCard'
import { SecretField } from './SecretField'
import { IgdbClientId } from './IgdbClientId'

type Provider = OperationalStatus['providers'][number]

// What each provider needs from an operator: its stored secret (if any) and where the rest comes from.
const needs: Record<string, { logo: string; secret?: string; label?: string; help?: string }> = {
    tmdb: { logo: 'tmdb', secret: 'tmdb_read_token', label: 'TMDb read token', help: 'Movies and TV. Use the API Read Access Token from TMDB API settings, not the v3 API key.' },
    musicbrainz: { logo: 'musicbrainz', help: 'Music. Public and read-only: nothing to configure.' },
    igdb: { logo: 'igdb', secret: 'igdb_client_secret', label: 'IGDB client secret', help: 'Games. Needs both the client ID and the client secret from your Twitch application; both are saved here.' },
    apprise: { logo: 'apprise', secret: 'apprise_config_key', label: 'Apprise configuration key', help: 'Outbound notifications. The server address is REQUESTARR_APPRISE_URL; the configuration key is saved here.' },
}

export function ProviderIntegrations({ providers, keys, csrfToken, onRequestError }: { providers: Provider[]; keys: string[]; csrfToken: string; onRequestError: (error: unknown) => void }) {
    const shown = keys.map(key => providers.find(provider => provider.key === key)).filter((provider): provider is Provider => !!provider)
    return <ul className="integrations">{shown.map(provider => {
        const spec = needs[provider.key] ?? { logo: provider.key }
        const configured = provider.configured
        const status = provider.note ? provider.note : provider.key === 'musicbrainz' ? 'Ready' : configured ? 'Configured' : 'Not configured'
        return <IntegrationCard key={provider.key} slug={spec.logo} name={provider.name} status={status} tone={provider.note ? 'warn' : configured ? 'on' : 'off'} configurable={!!spec.secret} meta={spec.help} setUp={!!spec.secret && !configured}>
            {provider.key === 'igdb' && <IgdbClientId csrfToken={csrfToken} onRequestError={onRequestError} />}
            {spec.secret && <SecretField name={spec.secret} label={spec.label ?? spec.secret} csrfToken={csrfToken} onRequestError={onRequestError} />}
        </IntegrationCard>
    })}</ul>
}
