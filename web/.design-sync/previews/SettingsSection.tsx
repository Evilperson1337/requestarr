import { SettingsSection } from 'requestarr-ui'

const G = ({ children }: { children: React.ReactNode }) => <div style={{ background: 'var(--ground)', color: 'var(--text)', padding: 20 }}>{children}</div>

const WithControlsInner = () => <SettingsSection title="Approval policy" description="Choose which kinds of request are approved without an administrator.">
  <label style={{ display: 'flex', gap: 8, alignItems: 'center' }}><input type="checkbox" defaultChecked /> Movies</label>
  <label style={{ display: 'flex', gap: 8, alignItems: 'center' }}><input type="checkbox" /> Games</label>
  <button type="button" className="button">Save</button>
</SettingsSection>
const TitleOnlyInner = () => <SettingsSection title="Server logs"><p>Recent log lines appear here.</p></SettingsSection>
export const WithControls = () => <G><WithControlsInner /></G>
export const TitleOnly = () => <G><TitleOnlyInner /></G>
