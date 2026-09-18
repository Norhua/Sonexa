import { describe, expect, it } from 'vitest'
import { trackPosition } from '../../src/api'

describe('candidate track position', () => {
  it.each([
    [2, 12, '2/12'], [2, undefined, '2/—'], [undefined, 12, '—/12'],
    [null, null, '—'], [0, 0, '—'], [0, 12, '—/12'],
  ] as const)('formats %s of %s without inventing missing values', (n, total, result) => {
    expect(trackPosition(n, total)).toBe(result)
  })
})
