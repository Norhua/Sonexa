import { expect, it } from 'vitest'
import { lyricsText } from '../../src/lib/lyrics'
import { lyricReadingRows, lyricRubySegments, lyricFormat, type LyricDocument } from '../../src/lib/lyricsPresentation'

it('keeps XLRC language, voices and Unicode ruby separate from body text', () => {
  const rows: LyricDocument['rows'] = [{ timeMs: 1000, text: '😀私が', channel: 'original', voice: 'A', ruby: [{ start: 1, end: 2, reading: 'わたし' }], translation: ['I', 'Moi'], romanization: ['watashi'], annotations: [
    { channel: 'translation', language: 'en', text: 'I' }, { channel: 'translation', language: 'fr', text: 'Moi' }, { channel: 'romanization', language: 'ja-Latn', text: 'watashi' },
  ] }]
  const before = structuredClone(rows)
  const reading = lyricReadingRows(rows, 'translation')[0]!
  expect(reading.voice).toBe('A'); expect(reading.annotations?.map(a => a.language)).toEqual(['en', 'fr'])
  expect(lyricReadingRows(rows, 'romanization')[0]!.annotations?.map(a => a.language)).toEqual(['ja-Latn'])
  expect(lyricRubySegments(reading.text, reading.ruby)).toEqual([{ text: '😀' }, { text: '私', reading: 'わたし' }, { text: 'が' }])
  expect(rows).toEqual(before); expect(lyricFormat('xlrc')).toBe('XLRC')
})
it('displays saved XLRC without control syntax and never executes or deletes literal brackets', () => {
  const raw = '[xlrc:0.1]\n[x-sonexa:1]\n[lang:ja]\n[00:01.005][v:A][x-end:3000]<00:01.005>私[わたし]<00:02>が\n[>en]I\n[>ja-Latn]watashi\n[x-channel:translation:und]\n[x-text]未同步\n[x-channel:original:und]\n[x-text]\\[>literal] 私\\[かな] \\<00:01> \\<b>safe\\</b>'
  expect(lyricsText(raw)).toBe('私が\nI\nwatashi\n未同步\n[>literal] 私[かな] <00:01> <b>safe</b>')
  expect(lyricsText('[00:01][v:B]君[きみ]')).toBe('君')
  expect(lyricsText('ordinary [>en] and 私[かな]')).toBe('ordinary [>en] and 私[かな]')
})
