import { Modal } from 'requestarr-ui'

export const Dialog = () => <div style={{ transform: 'translateZ(0)', width: 720, height: 560 }}><Modal labelledBy="m-title" onClose={() => {}}>
  <h2 id="m-title">Report a problem</h2>
  <p>Tell the administrators what is wrong with Arrival (2016).</p>
  <label style={{ display: 'grid', gap: 6 }}>What is wrong?
    <select><option>Wrong subtitles</option><option>Audio out of sync</option><option>Missing episodes</option></select>
  </label>
  <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
    <button type="button" className="button">Report</button>
    <button type="button" className="button secondary">Cancel</button>
  </div>
</Modal></div>
