import { ApiError } from './api'

// The local administrator's password rule (D040): 12 to 128 characters and nothing else.
// The server is the authority; this only saves a round trip and words the answers.
export const PASSWORD_MIN = 12
export const PASSWORD_MAX = 128

// validateNewPassword returns the first problem with a new password form, or '' when it is fine.
export function validateNewPassword(current: string, next: string, confirm: string): string {
    const length = [...next].length
    if (!current) return 'Enter your current password.'
    if (length < PASSWORD_MIN || length > PASSWORD_MAX) return `Choose a new password of ${PASSWORD_MIN} to ${PASSWORD_MAX} characters. A passphrase works well.`
    if (next !== confirm) return 'The new password and its confirmation do not match.'
    if (next === current) return 'Choose a password different from the current one.'
    return ''
}

export function tooManyAttemptsMessage(error: ApiError): string {
    return `Too many attempts. Try again in ${error.retryAfter || 'a few'} seconds.`
}
