import { ProviderLogo } from 'requestarr-ui'

const G = ({ children }: { children: React.ReactNode }) => <div style={{ background: 'var(--ground)', color: 'var(--text)', padding: 20 }}>{children}</div>

const row = (slugs: string[], size?: number) => <div style={{ display: 'flex', gap: 16, alignItems: 'center' }}>{slugs.map(s => <ProviderLogo key={s} slug={s} size={size} />)}</div>

const MediaServersInner = () => row(['jellyfin', 'plex', 'navidrome', 'audiobookshelf', 'romm'])
const MetadataProvidersInner = () => row(['tmdb', 'musicbrainz', 'igdb', 'apprise'])
const LargeInner = () => row(['jellyfin', 'tmdb'], 64)
const UnknownSlugInner = () => row(['not-a-service'])
export const MediaServers = () => <G><MediaServersInner /></G>
export const MetadataProviders = () => <G><MetadataProvidersInner /></G>
export const Large = () => <G><LargeInner /></G>
export const UnknownSlug = () => <G><UnknownSlugInner /></G>
