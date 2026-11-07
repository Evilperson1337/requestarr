import apprise from '../assets/providers/apprise.svg'
import audiobookshelf from '../assets/providers/audiobookshelf.svg'
import igdb from '../assets/providers/igdb.svg'
import jellyfin from '../assets/providers/jellyfin.svg'
import musicbrainz from '../assets/providers/musicbrainz.svg'
import navidrome from '../assets/providers/navidrome.png'
import plex from '../assets/providers/plex.svg'
import romm from '../assets/providers/romm.svg'
import tmdb from '../assets/providers/themoviedatabase.svg'

const logos: Record<string, string> = { apprise, audiobookshelf, igdb, jellyfin, musicbrainz, navidrome, plex, romm, tmdb }

export const hasProviderLogo = (slug: string) => slug in logos

// The service's own mark, decorative: its name always sits beside it.
export function ProviderLogo({ slug, size = 32 }: { slug: string; size?: number }) {
    const src = logos[slug]
    if (!src) return <span className="logoBlank" style={{ width: size, height: size }} aria-hidden="true" />
    return <img className="providerLogo" src={src} width={size} height={size} alt="" aria-hidden="true" />
}
