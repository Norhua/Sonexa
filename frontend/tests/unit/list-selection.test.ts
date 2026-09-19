import { describe, expect, it } from 'vitest'
import { effectScope, ref } from 'vue'
import { useListSelection } from '../../src/lib/useListSelection'
import { listRows, sortRows, uniqueTracks } from '../../src/lib/trackRows'

function fixture() {
  const scope = effectScope()
  const rows = ref(listRows([
    { id: 1, title: '曲10' }, { id: 0, title: 'ghost', ghost: true },
    { id: 2, title: '曲2' }, { id: 3, title: '曲1' }, { id: 4, title: '曲4' },
  ]))
  const select = scope.run(() => useListSelection(rows))!
  const ids = () => select.selectedRows.value.map(row => row.id)
  return { rows, select, ids }
}
describe('file list selection', () => {
  it('plain click replaces, Meta/Ctrl toggles', () => {
    const { select, ids } = fixture()
    select.choose('track:1'); select.choose('track:2', { metaKey: true })
    expect(ids()).toEqual([1, 2])
    select.choose('track:1', { ctrlKey: true }); expect(ids()).toEqual([2])
    select.choose('track:4'); expect(ids()).toEqual([4])
  })
  it('Shift range shrinks, skips ghosts and preserves anchor', () => {
    const { select, ids } = fixture()
    select.choose('track:1'); select.choose('track:4', { shiftKey: true })
    expect(ids()).toEqual([1, 2, 3, 4])
    select.choose('track:2', { shiftKey: true })
    expect(ids()).toEqual([1, 2]); expect(select.anchor.value).toBe('track:1')
  })
  it('Ctrl+Shift adds a range, shrinking does not erase the base selection', () => {
    const { select, ids } = fixture()
    select.choose('track:1'); select.choose('track:3', { ctrlKey: true })
    select.choose('track:4', { ctrlKey: true, shiftKey: true })
    expect(ids()).toEqual([1, 3, 4])
    select.choose('track:3', { ctrlKey: true, shiftKey: true })
    expect(ids()).toEqual([1, 3])
  })
  it('sort changes range order, not anchor identity', () => {
    const { rows, select, ids } = fixture()
    select.choose('track:1')
    rows.value = sortRows(rows.value, 'title', false)
    select.choose('track:4', { shiftKey: true })
    expect(ids()).toEqual([4, 1])
  })
  it('removed anchor cannot select an unrelated stale interval', () => {
    const { rows, select, ids } = fixture()
    select.choose('track:1'); rows.value = rows.value.filter(row => row.id !== 1)
    expect(ids()).toEqual([]); expect(select.anchor.value).toBeNull()
    select.choose('track:2', { shiftKey: true }); expect(ids()).toEqual([2])
  })
  it('keyboard skips ghost, moves selection, extends and contracts', () => {
    const { select, ids } = fixture()
    select.choose('track:1'); select.move('down'); expect(ids()).toEqual([2])
    select.move('down', { shiftKey: true }); expect(ids()).toEqual([2, 3])
    select.move('up', { shiftKey: true }); expect(ids()).toEqual([2])
    select.move('last'); expect(ids()).toEqual([4])
    select.move('first', { ctrlKey: true }); expect(ids()).toEqual([4])
    expect(select.focused.value).toBe('track:1')
  })
  it('right-click selected preserves selection, ghost never changes it', () => {
    const { rows, select, ids } = fixture()
    select.all(); select.context('track:1'); expect(ids()).toEqual([1, 2, 3, 4])
    select.choose(rows.value[1].key); select.context(rows.value[1].key)
    expect(ids()).toEqual([1, 2, 3, 4])
    select.clear(); expect(ids()).toEqual([])
  })
  it('background reload replaces row objects but preserves live keys', () => {
    const { rows, select, ids } = fixture()
    select.choose('track:2'); rows.value = rows.value.map(row => ({ ...row, title: 'new' }))
    expect(ids()).toEqual([2]); expect(select.selectedRows.value[0].title).toBe('new')
  })
})
describe('track rows', () => {
  it('positions are unique while exported tracks are deduplicated', () => {
    const rows = listRows([
      { id: 0, title: 'ghost1', ghost: true, trackNumber: 1 },
      { id: 0, title: 'ghost2', ghost: true, trackNumber: 2 },
      { id: 7, title: 'repeat', rowKey: 'position:3' },
      { id: 7, title: 'repeat', rowKey: 'position:4' },
    ])
    expect(new Set(rows.map(row => row.key)).size).toBe(4)
    expect(uniqueTracks(rows).map(row => row.id)).toEqual([7])
  })
  it('natural stable sorting and null-last numeric sorting', () => {
    const rows = listRows([{ id: 1, title: '曲10', year: 2010 }, { id: 2, title: '曲2' }, { id: 3, title: '曲2', year: 2020 }])
    expect(sortRows(rows, 'title', false).map(row => row.id)).toEqual([2, 3, 1])
    expect(sortRows(rows, 'year', true).map(row => row.id)).toEqual([3, 1, 2])
    expect(sortRows(rows, 'default', true)).toBe(rows)
  })
})
