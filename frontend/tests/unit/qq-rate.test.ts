import { expect, it } from 'vitest'
import { QQ_RATE_MIN, QQ_RATE_MAX, validQQRate } from '../../src/lib/qqRate'

it('accepts both QQ boundaries, decimals and existing lower rates without replacing them', () => {
  expect([QQ_RATE_MIN, QQ_RATE_MAX]).toEqual([1, 60])
  for (const rate of [1, 20, 20.5, 30, 45, 60]) expect(validQQRate(rate)).toBe(true)
})

it('rejects empty, nonnumeric, nonfinite and out-of-range QQ values', () => {
  for (const rate of [null, undefined, '', '30', NaN, Infinity, -Infinity, 0, -1, 0.5, 60.1]) {
    expect(validQQRate(rate), String(rate)).toBe(false)
  }
})
