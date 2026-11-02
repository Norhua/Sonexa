# Missing-version search regression

`search_version_recall.json` is a real MusicBrainz JSON response captured during change-record round 3. Recording/release/track titles, identifiers and positions are unmodified (only JSON pretty-printing applied).

- Request: `GET https://musicbrainz.org/ws/2/recording?fmt=json&limit=25&query=recording%3A%22%E6%82%B2%E3%81%97%E3%81%8F%E3%81%A6+lulululu%22`
- Three recordings, each with four release tracks (12 tracks total).
- Hayasaka recording: `c62b67ba-53b0-4fbf-88f8-040cf7f9a55d`.
- Requested Japanese CD release: `24da5eb0-ac4b-4298-b827-6c0cbef391f7`.
- Target track (disc 1, track 2): `d2895728-1e49-42e6-81fd-75e1be292099`.
- Two releases use romanized track titles despite a Japanese recording title; one uses `ハーサカver.` without a word-space boundary.
- Tests replay this through the real provider parser, matching engine and application bindings without public network access. The handler deliberately returns empty results for anchored queries to exercise the existing title-only fallback.

MusicBrainz core metadata is CC0: https://musicbrainz.org/doc/About/Data_License
