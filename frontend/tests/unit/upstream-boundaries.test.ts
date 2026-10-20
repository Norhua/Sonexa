import { readFileSync } from 'node:fs'
import { expect, it } from 'vitest'
import ts from 'typescript'

// This supplements behavior tests: no second list or source-ID branch belongs
// in lyric/cover orchestration. Adapter settings are intentionally out of scope.
it('keeps platform IDs out of cover and lyric business scripts and templates', () => {
  const ids = new Set(['qq', '163', 'itunes', 'musicbrainz', 'caa', 'kugou', 'kuwo'])
  for (const path of ['stores/coverSearch.ts', 'stores/lyricsSearch.ts', 'lib/sourceSelection.ts', 'views/CoverSearchView.vue', 'views/LyricsSearchView.vue']) {
    const body = readFileSync(new URL(`../../src/${path}`, import.meta.url), 'utf8')
    const check = (source: string) => {
      const tree = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true)
      const visit = (node: ts.Node) => {
        if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
          expect(ids.has(node.text), `${path}: platform literal ${node.text}`).toBe(false)
        }
        ts.forEachChild(node, visit)
      }
      visit(tree)
    }
    if (path.endsWith('.vue')) {
      check(body.match(/<script[^>]*>([\s\S]*?)<\/script>/)?.[1] ?? '')
      const template = body.slice(body.indexOf('<template>')).replace(/<!--[\s\S]*?-->/g, '')
      for (const attribute of template.matchAll(/(?:v-[\w:-]+|[:@][\w:-]+)="([^"]*)"/g)) check(attribute[1]!)
    } else check(body)
  }
})
