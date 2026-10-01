// Wails 桩件：让前端在普通 Chrome 里跑起来，用于核对交互。
// 只实现界面用到的绑定，返回形状与 Go 侧一致的假数据。
(() => {
  const listeners = {}
  const state = {
    enrich: { state: 'idle', done: 4, total: 7, pending: 3, skipped: 1, reason: '', lastError: '' },
  }

  const TITLES = [
    ['綿雪の夢', 'ハトリアヤ', '狼と巫女とお菓子の家'],
    ['夕立', "CROW'SCLAW", 'アコースティック・エピソード'],
    ['Tetra.', '原田ひとみ', 'Trinoline Original Soundtrack ~LINES~'],
    ['世界中の雨', 'チームドラゴン from AKB48', '心の羽根'],
    ['By the Seaside', 'Kirara Magic feat. Kitsui Akira', 'By the Seaside'],
    ['3253212', 'reche', '3253212'],
    ['God knows...', '平野綾', '涼宮ハルヒの詰合'],
  ]
  const tracks = TITLES.map(([title, artist, album], i) => ({
    id: i + 1, title, artist, album,
    year: 2010 + i, trackNumber: i + 1, discNumber: 1,
    durationMs: 200000 + i * 1000,
    genre: null, mediumFormat: 'CD', country: 'JP',
    matchSource: null, releaseRef: null, releaseGroupRef: null, coverPath: null,
    titleLocked: false, artistLocked: false, albumLocked: false,
    trackTotal: TITLES.length, primaryType: 'Album', matchRef: null,
  }))

  // 形状必须与 wailsjs/go/models.ts 的 config.Settings 一致——
  // 键名对不上时设置弹窗会整块渲染不出来，而那正是要核对的东西。
  const settings = {
    general: { theme: 'dark', language: 'zh-CN' },
    musicbrainz: { contact: 'test@example.com', serverUrl: '', ratePerSec: 1 },
    itunes: { defaultCountry: 'JP', ratePerMin: 20 },
    qqmusic: { ratePerMin: 30 },
    providerPriority: ['musicbrainz', 'itunes', 'qq'],
    matching: {
      titleWeight: 40, creditExactWeight: 25, creditFuzzyWeight: 15,
      albumExactWeight: 20, albumFuzzyWeight: 10, upstreamScoreScale: 1,
      upstreamScoreCap: 20, stripPenalty: 5, jaroWinklerMin: 0.9,
      reviewThreshold: 70, preferredCountries: ['JP'], mediumPriority: ['CD'],
      maxRetries: 3, backoffBaseSeconds: 2,
      variantStrategy: 'progressive', tracklistTiming: 'on_album_open',
    },
    importExport: {
      defaultEncoding: 'utf-8', defaultDelimiter: ',', multiValueSep: ';',
      exportFormat: 'csv', exportUtf8Bom: true,
    },
    cache: {
      coverDir: '', coverMaxBytes: 536870912, coverConcurrency: 32,
      providerCacheTtlHours: 24, coverTtlDays: 30,
    },
    onboardingDone: true,
  }

  window.__calls = []
  const record = (name, fn) => async (...args) => {
    window.__calls.push(name)
    return fn(...args)
  }

  window.go = {
    app: {
      App: {
        GetSettings: async () => settings,
        DefaultSettings: async () => settings,
        SaveSettings: async next => {
          Object.assign(settings, JSON.parse(JSON.stringify(next)))
          emit('settings:saved', settings)
        },
        ListProviders: async () => [
          { id: 'musicbrainz', displayName: 'MusicBrainz', configured: true, reason: '', capabilities: ['label'], needsCountry: false },
          { id: 'itunes', displayName: 'iTunes', configured: true, reason: '', capabilities: [], needsCountry: true },
          { id: 'qq', displayName: 'QQ 音乐', configured: true, reason: '', capabilities: [], needsCountry: false },
        ],
        CheckOutbound: async () => ({ allowed: !!settings.musicbrainz.contact.trim(), reason: settings.musicbrainz.contact.trim() ? '' : '需要设置联系方式', settingsAnchor: 'upstream' }),
        CheckReachable: async () => true,
        ListCollections: async kind => kind === 'playlist' ? [{ id: 10, kind, name: '测试歌单', trackCount: tracks.length }] : [],
        ListTracks: async filter => {
          const list = filter?.query ? tracks.filter(t => t.title.includes(filter.query)) : tracks
          return { tracks: list, total: list.length }
        },
        ReorderCollection: async (id, ids) => {
          window.__lastReorder = { id, ids }
          tracks.sort((a, b) => ids.indexOf(a.id) - ids.indexOf(b.id))
        },
        GetTaskSummary: record('GetTaskSummary', async () => ({ activeBatch: 0, filename: '', processed: 0, total: 0, stats: null, running: false, reviewPending: 3 })),
        GetEnrichStatus: record('GetEnrichStatus', async () => state.enrich),
        StartEnrich: async () => {
          state.enrich = { ...state.enrich, state: 'running' }
          emit('enrich:progress', state.enrich)
        },
        PauseEnrich: async () => {
          state.enrich = { ...state.enrich, state: 'paused' }
          emit('enrich:progress', state.enrich)
        },
        GetTrackDetail: async (id) => ({
          track: tracks.find((t) => t.id === id) ?? tracks[0],
          participants: [], creditSpans: [], tags: [], provenance: {},
        }),
        CoverOutcomes: async queries => queries.map(() => 'empty'),
        CoverTargetOutcomes: async queries => queries.map(() => 'empty'),
        CoverRevision: async () => 0,
        ListCoverSources: async () => [
          { id: 'caa', name: 'CAA', needsCountry: false },
          { id: 'itunes', name: 'iTunes', needsCountry: true },
          { id: 'qq', name: 'QQ', needsCountry: false },
          { id: '163', name: '网易云', needsCountry: false },
        ],
        StartCoverSearch: async request => {
          for (const source of request.sources) emit('cover:results', { generation: request.generation, searchId: request.searchId, releaseId: request.releaseId, source, state: 'empty', cards: [], error: '' })
        },
        CoverSearchResults: async () => [],
        CancelCoverSearch: async () => {},
        PickAndAdoptCover: async () => ({ cancelled: true }),
        ClearCover: async (releaseId) => ({ releaseId, revision: 1 }),
        BuildCoverRefs: async () => [],
        CancelCovers: async () => {},
        PrefetchCovers: async () => {},
        ListCountries: async () => [{ code: 'JP', name: '日本' }],
        SuggestCountry: async () => ({ detected: false, country: '', message: '' }),
        ExportTracks: async (req) => {
          window.__lastExport = req
          return { path: '/tmp/export.csv', count: (req.trackIds || []).length }
        },
        GetReviewQueue: async () => ({
          items: [
            {
              id: 11, batchId: 1, status: 'review',
              inTitle: 'ツバサ（羽翼）', inArtist: '4円（ヨエン）', inAlbum: 'side-c',
              batchTag: 'music_extracted.csv', triedVariants: ['ツバサ', '羽翼'],
              candidates: [], attemptCount: 0, lastError: null, lastErrorAt: null,
            },
            {
              id: 12, batchId: 1, status: 'review',
              inTitle: 'you', inArtist: '茅野愛衣（かやの あい）', inAlbum: '冴えない彼女の育てかた 第4巻 特典C...',
              batchTag: 'music_extracted.csv', triedVariants: ['you'],
              candidates: [], attemptCount: 0, lastError: null, lastErrorAt: null,
            },
          ],
          total: 2, pending: 2, failed: 0,
        }),
        ResearchReviewItem: record('ResearchReviewItem', async (id, title, artist, album, providerID) => {
          window.__lastResearch = { id, title, artist, album, providerID }
          return [
            {
              provider: providerID, matchRef: 'trk-' + providerID, badges: [providerID],
              note: '', action: '接受', localTrackId: 0, thumbKey: '',
              title: title || '候选', artist: artist || null, album: album || null,
              primaryType: 'Album', durationMs: 200000, trackNumber: 1, trackTotal: 10,
              year: 2011, mediumFormat: 'CD', country: 'JP', label: null, catalogNumber: null,
              confidence: 75, matchedVariant: '锚定', artworkUrl: null,
              releaseRef: 'rel-1', releaseGroupRef: 'rg-1', recordingRef: 'rec-1', isrc: null,
            },
          ]
        }),
        DiscardReviewItem: record('DiscardReviewItem', async () => {}),
        PrepareNoMatchNeeded: record('PrepareNoMatchNeeded', async () => ({ id: 'no-match', hash: 'hash', baseline: { conflict: false, requiresConfirmation: false } })),
        CommitAdmission: record('CommitAdmission', async () => ({ trackId: 1 })),
        GetImportQueue: async () => [{
          id: 1, filename: '/tmp/music_extracted.csv', status: 'done',
          running: false, processed: 10, totalRows: 10, fileRows: 12,
          stats: { imported: 7, review: 3, queued: 0, failed: 0 },
          failedTitles: [], createdAt: '2026-09-16T01:00:00Z',
        }],
        PickImportFile: async () => '/tmp/music_extracted.csv',
        AnalyzeImportFile: async () => ({
          statusLine: '已自动识别：UTF-8 / 逗号分隔 / 首行为表头',
          encodings: ['utf-8', 'gbk'],
          detection: { encoding: 'utf-8', delimiterRune: ',', hasHeader: true,
            needsAttention: false, reason: '', sheetNames: [] },
          headers: ['歌曲名', '歌手名', '专辑名'],
          columnMap: ['title', 'artist', 'album'],
          fieldOptions: [
            { value: 'title', label: '曲名' }, { value: 'artist', label: '艺术家' },
            { value: 'album', label: '专辑' }, { value: '', label: '忽略' },
          ],
          preview: { totalRows: 10, importableRows: 10, skipSummary: '',
            rows: TITLES.map(([title, artist, album], i) => ({
              index: i, title, artist, album, year: null, trackNo: null, skipReason: '' })) },
        }),
        RemapImportColumns: async (...a) => window.go.app.App.AnalyzeImportFile(...a),
        SubmitImport: async () => 2,
        GetEditSession: async (trackId) => ({
          matchLabel: 'MusicBrainz · rec-1', matchSource: 'musicbrainz', matchRef: 'trk-1',
          fields: [
            { key: 'title', label: '曲名', group: 'basic', current: '夕立',
              upstream: '夕立 (Remastered)', same: false, userLocked: false,
              fillByDefault: true, releaseScoped: false },
            { key: 'artist', label: '艺术家', group: 'basic', current: "CROW'SCLAW",
              upstream: "CROW'SCLAW", same: true, userLocked: true,
              fillByDefault: false, releaseScoped: false },
            { key: 'album', label: '歌集名', group: 'rich', current: 'アコースティック・エピソード',
              upstream: null, same: false, userLocked: false,
              fillByDefault: false, releaseScoped: true },
          ],
        }),
        PlanAlbumRename: async () => ({ needsConfirm: false, trackCount: 1, currentTitle: '', existingReleaseId: 0 }),
        ApplyEdits: async () => {},
        ClearProvenance: async () => {},
        ListArtists: async () => [],
        ListGroupProposals: async () => [],
        ListAlbums: async () => [{ releaseId: 20, groupId: 20, title: '测试专辑', ownedCount: 2, memberCount: 1, ownedReleaseCount: 1, totalCount: 5 }],
        GetAlbumGroupDetail: async (groupId) => {
          const release = await window.go.app.App.GetAlbumDetail(groupId)
          return { groupId, title: release.title, representativeId: release.releaseId, ownedCount: release.ownedCount, memberCount: 1, releases: [release] }
        },
        ListAlbumsForTag: async () => window.go.app.App.ListAlbums(),
        GetAlbumDetail: async () => ({
          releaseId: 20, title: '测试专辑', ownedCount: 2, totalCount: 5, tracklistState: 'complete',
          rows: [
            { trackId: 1, title: '已拥有 A', discNumber: 1, trackNumber: 1, ghost: false },
            { trackId: 0, title: '缺少 B', discNumber: 1, trackNumber: 2, ghost: true },
            { trackId: 2, title: '已拥有 C', discNumber: 1, trackNumber: 3, ghost: false },
            { trackId: 0, title: 'A 的另一版本（未入库）', discNumber: 1, trackNumber: 4, ghost: true, state: 'missing' },
            { trackId: 0, title: '缺少 D', discNumber: 1, trackNumber: 5, ghost: true },
          ],
        }),
        DeleteTracks: async () => {},
        About: async () => ({ version: 'harness', attributions: [] }),
      },
    },
  }

  function emit(name, ...data) {
    for (const fn of listeners[name] ?? []) fn(...data)
  }

  const on = (name, fn) => {
    ;(listeners[name] ??= []).push(fn)
    return () => {
      listeners[name] = (listeners[name] ?? []).filter((f) => f !== fn)
    }
  }

  window.runtime = {
    // wailsjs 的 EventsOn 实际转调 EventsOnMultiple，桩件必须提供它
    EventsOnMultiple: (name, fn) => on(name, fn),
    EventsOn: (name, fn) => on(name, fn),
    EventsOnce: (name, fn) => on(name, fn),
    EventsOff: (name) => { delete listeners[name] },
    EventsEmit: emit,
    LogPrint: () => {},
    WindowSetDarkTheme: () => {},
    WindowSetLightTheme: () => {},
    Environment: async () => ({ platform: 'darwin' }),
  }
})()
