// Every stored-capable secret, in display order. The Settings page renders one
// SecretField per entry; names must match the registry in internal/secrets.
// The setup wizard places a field next to each step's own inputs instead.
export const storedSecrets = [
    { name: 'tmdb_read_token', label: 'TMDb read token' },
    { name: 'igdb_client_secret', label: 'IGDB client secret' },
    { name: 'apprise_config_key', label: 'Apprise configuration key' },
    { name: 'jellyfin_api_key', label: 'Jellyfin API key' },
    { name: 'plex_token', label: 'Plex token' },
    { name: 'romm_token', label: 'RomM token' },
    { name: 'navidrome_password', label: 'Navidrome password' },
] as const
