import { useCallback, useState } from 'react'
import { ApiError, type Session } from '../api'
import { DiagnosticsTokens } from '../components/DiagnosticsTokens'
import { ServerLogs } from '../components/ServerLogs'

// Logs (D044): the server's own recent records for administrators and superusers.
// Diagnostics tokens (D043) stay with the superuser, who alone may create them.
export function LogsPage({ session }: { session: Session }) {
    const [lost, setLost] = useState(false)
    const checkAuthorization = useCallback((error: unknown) => {
        if (error instanceof ApiError && (error.status === 401 || error.status === 403)) setLost(true)
    }, [])
    return <main className="shell settingsPage"><h1>Logs</h1>
        {lost ? <p role="alert" className="error">Your access to the logs has changed. Refresh the page to continue.</p> : <>
            {session.capabilities.manageAccess && <DiagnosticsTokens csrfToken={session.csrfToken} onRequestError={checkAuthorization} />}
            <ServerLogs onRequestError={checkAuthorization} />
        </>}
    </main>
}
