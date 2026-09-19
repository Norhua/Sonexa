import { describe, expect, it } from 'vitest'
import { addedTime, trackPosition } from '../../src/api'
import { listRows, sortRows } from '../../src/lib/trackRows'

describe('added time', () => {
  it('formats only the local date as yyyy-mm-dd, keeping missing or invalid values empty', () => {
    for (const value of [null, undefined, '', 'invalid']) expect(addedTime(value)).toBe('—')
    for (const hour of [0, 13, 23]) {
      const date = new Date(2026, 8, 18, hour, 4, 5)
      expect(addedTime(date.toISOString())).toBe('2026-09-18')
    }
    expect(addedTime(new Date(2026, 0, 2, 12).toISOString())).toBe('2026-01-02')
  })
  it('sorts chronologically across offsets and keeps missing values last', () => {
    const rows = listRows([
      { id: 1, title: 'later', addedAt: '2026-09-18T02:00:00Z' },
      { id: 2, title: 'earlier', addedAt: '2026-09-18T09:00:00+08:00' },
      { id: 3, title: 'invalid', addedAt: 'bad' },
      { id: 4, title: 'missing' },
    ])
    expect(sortRows(rows, 'addedAt', false).map(row => row.id)).toEqual([2, 1, 3, 4])
    expect(sortRows(rows, 'addedAt', true).map(row => row.id)).toEqual([1, 2, 3, 4])
  })
})

describe('candidate track position', () => {
  it.each([
    [2, 12, '2/12'], [2, undefined, '2/—'], [undefined, 12, '—/12'],
    [null, null, '—'], [0, 0, '—'], [0, 12, '—/12'],
  ] as const)('formats %s of %s without inventing missing values', (n, total, result) => {
    expect(trackPosition(n, total)).toBe(result)
  })
})
