import { BrandMark } from 'requestarr-ui'

const G = ({ children }: { children: React.ReactNode }) => <div style={{ background: 'var(--ground)', color: 'var(--text)', padding: 20 }}>{children}</div>

const DefaultInner = () => <BrandMark />
const LargeInner = () => <BrandMark size={64} />
const BesideNameInner = () => <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 20, fontWeight: 600 }}><BrandMark size={28} />Requestarr</div>
export const Default = () => <G><DefaultInner /></G>
export const Large = () => <G><LargeInner /></G>
export const BesideName = () => <G><BesideNameInner /></G>
