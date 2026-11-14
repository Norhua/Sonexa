import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useReviewStore } from '../../src/stores/review'

const items = (...ids: number[]) => ids.map(id => ({ id }))
beforeEach(() => setActivePinia(createPinia()))

describe('review session cursor', () => {
  it('starts at the first item and retains the cursor across consumers', () => {
    const review = useReviewStore()
    expect(review.reconcile(items(11, 12, 13))).toBe(0)
    review.remember(12, 1)
    const reopened = useReviewStore()
    expect(reopened.reconcile(items(11, 12, 13))).toBe(1)
    expect(reopened.itemId).toBe(12)
  })

  it('keeps the item by ID after insertions, removals and reordering', () => {
    const review = useReviewStore()
    review.remember(12, 1)
    expect(review.reconcile(items(9, 11, 12, 13))).toBe(2)
    expect(review.reconcile(items(12, 13))).toBe(0)
    expect(review.itemId).toBe(12)
  })

  it('uses the old position if missing and clamps a missing last item', () => {
    const review = useReviewStore()
    review.remember(12, 1)
    expect(review.reconcile(items(11, 13, 14))).toBe(1)
    expect(review.itemId).toBe(13)
    review.remember(14, 2)
    expect(review.reconcile(items(11, 13))).toBe(1)
    expect(review.itemId).toBe(13)
  })

  it('clears an empty queue and does not retain a stale cursor for later imports', () => {
    const review = useReviewStore()
    review.remember(12, 1)
    expect(review.reconcile([])).toBe(0)
    expect(review.itemId).toBeNull()
    expect(review.index).toBe(0)
    expect(review.reconcile(items(21, 22))).toBe(0)
    expect(review.itemId).toBe(21)
  })
})
