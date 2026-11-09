import { IntegrationCard } from 'requestarr-ui'

const G = ({ children }: { children: React.ReactNode }) => <div style={{ background: 'var(--ground)', color: 'var(--text)', padding: 20 }}>{children}</div>

const list = (children: React.ReactNode) => <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 12, maxWidth: 640 }}>{children}</ul>

const WorkingInner = () => list(<IntegrationCard slug="jellyfin" name="Jellyfin" status="Connected" tone="on" meta="Last synced 4 minutes ago: 1,204 items">
  <p>Server URL and API key are set.</p>
</IntegrationCard>)
const NeedsSetupInner = () => list(<IntegrationCard slug="plex" name="Plex" status="Not set up" tone="off" setUp meta="Add a server URL and token to start syncing.">
  <p>Enter your Plex server details.</p>
</IntegrationCard>)
const WarningInner = () => list(<IntegrationCard slug="romm" name="RomM" status="Last sync failed" tone="warn" meta="The server did not respond." actions={<button type="button" className="button secondary">Sync now</button>} />)
const OpenSettingsInner = () => list(<IntegrationCard slug="tmdb" name="TMDB" status="Connected" tone="on" defaultOpen>
  <p>API key stored. Changes apply at once.</p>
</IntegrationCard>)
const NotConfigurableInner = () => list(<IntegrationCard slug="apprise" name="Apprise" status="Ready" tone="on" configurable={false} meta="Notifications go through the global outbox." />)
export const Working = () => <G><WorkingInner /></G>
export const NeedsSetup = () => <G><NeedsSetupInner /></G>
export const Warning = () => <G><WarningInner /></G>
export const OpenSettings = () => <G><OpenSettingsInner /></G>
export const NotConfigurable = () => <G><NotConfigurableInner /></G>
