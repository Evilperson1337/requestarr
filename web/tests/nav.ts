import type { Page } from '@playwright/test'

// Below 900px the main navigation is a drawer behind a Menu button; open it if it is closed.
export async function openNav(page: Page) {
    const toggle = page.getByRole('button', { name: 'Menu', exact: true })
    if (await toggle.isVisible()) await toggle.click()
}
