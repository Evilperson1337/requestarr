import { expect, type Page } from '@playwright/test'

// At phone width every control a thumb must hit is at least 44px high (AGENTS.md: accessibility and
// responsive layout). Buttons, selects and text inputs count by their own box; a checkbox, radio or
// switch counts by its label, which is what a thumb touches. Text links in running prose are not controls.
export async function expectTouchTargets(page: Page, minimum = 44) {
    const small = await page.evaluate(minimum => {
        const label = (el: Element) => (el as HTMLElement).tagName === 'INPUT' && /^(checkbox|radio)$/.test((el as HTMLInputElement).type) ? el.closest('label') ?? (el as HTMLInputElement).labels?.[0] ?? el : el
        const visible = (el: Element) => {
            const box = el.getBoundingClientRect(), style = getComputedStyle(el)
            return box.width > 0 && box.height > 0 && style.visibility !== 'hidden' && !el.closest('[hidden], .sr-only')
        }
        const describe = (el: Element) => `${el.tagName.toLowerCase()}${el.id ? '#' + el.id : ''}${el.getAttribute('aria-label') ? `[${el.getAttribute('aria-label')}]` : ''} "${(el.textContent || (el as HTMLInputElement).value || '').trim().slice(0, 30)}"`
        return [...document.querySelectorAll('button, select, input:not([type=hidden]), [role=switch]')]
            .filter(visible)
            .map(el => [el, label(el)] as const)
            .filter(([, target]) => target.getBoundingClientRect().height < minimum)
            .map(([el, target]) => `${describe(el)} is ${Math.round(target.getBoundingClientRect().height)}px high`)
    }, minimum)
    expect([...new Set(small)], `controls under ${minimum}px at phone width`).toEqual([])
}
