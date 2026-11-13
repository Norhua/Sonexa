import { describe, expect, it } from 'vitest'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import CandidateAlbumName from '@/components/CandidateAlbumName.vue'
import { candidateReleaseState } from '@/lib/candidateReleaseState'

describe('candidate release state', () => {
  it.each([
    [true, true, 'text-green-700 dark:text-green-400', 'release 已存在'],
    [true, false, 'text-green-700 dark:text-green-400', 'release 已存在'],
    [false, true, 'text-orange-700 dark:text-orange-400', 'release group 已存在'],
    [false, false, '', ''],
    [undefined, undefined, '', ''],
  ])('release=%s group=%s uses the more specific identity', (releaseExists, releaseGroupExists, className, label) => {
    expect(candidateReleaseState({ releaseExists, releaseGroupExists })).toMatchObject({ className, label })
  })

  it('keeps the full album/type tooltip and a non-color explanation', async () => {
    const html = await renderToString(createSSRApp(CandidateAlbumName, {
      album: '完整专辑名（特别版）', detail: '(Album)', releaseGroupExists: true,
    }))
    expect(html).toContain('完整专辑名（特别版） · (Album) · release group 已存在')
    expect(html).toContain('class="sr-only"')
    expect(html).toContain('同来源的原生 release group 身份')
    expect(html).toContain('包含同步歌单')
    expect(html).toContain('不表示该发行或曲目已存在、已入库或可自动导入')
  })

  it('preserves the caller fallback without inventing an existence label', async () => {
    const html = await renderToString(createSSRApp(CandidateAlbumName, { album: null, fallback: '未知专辑' }))
    expect(html).toContain('未知专辑')
    expect(html).not.toContain('已存在')
    expect(html).not.toContain('text-green')
    expect(html).not.toContain('text-orange')
  })
})
