import { test as base, expect } from '@playwright/test'
export { expect }
export type { Page } from '@playwright/test'
export const test = base.extend<{ pageErrors: void }>({
  pageErrors: [async ({ page }, use) => {
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await use()
    expect(errors, 'unexpected browser runtime errors').toEqual([])
  }, { auto: true }],
})
