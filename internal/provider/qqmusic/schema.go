package qqmusic

import "encoding/json"

// block 是 musicu.fcg 响应中的一个请求块；块名由请求方指定。
type block struct {
	Code *int            `json:"code"`
	Data json.RawMessage `json:"data"`
}

type singer struct {
	ID    int64  `json:"id"`
	MID   string `json:"mid"`
	Name  string `json:"name"`
	Title string `json:"title"`
}

type albumRef struct {
	ID         int64  `json:"id"`
	MID        string `json:"mid"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	TimePublic string `json:"time_public"`
}

// song 是搜索结果、专辑清单与歌曲详情共用的歌曲结构。
type song struct {
	ID         int64    `json:"id"`
	MID        string   `json:"mid"`
	Name       string   `json:"name"`
	Title      string   `json:"title"`
	Singer     []singer `json:"singer"`
	Album      albumRef `json:"album"`
	Interval   int      `json:"interval"`
	IndexAlbum int      `json:"index_album"`
	IndexCD    int      `json:"index_cd"`
	TimePublic string   `json:"time_public"`
}

type searchData struct {
	Body *struct {
		Song *struct {
			List []song `json:"list"`
		} `json:"song"`
	} `json:"body"`
}

type albumData struct {
	BasicInfo *struct {
		AlbumMID    string `json:"albumMid"`
		AlbumName   string `json:"albumName"`
		PublishDate string `json:"publishDate"`
		Genre       string `json:"genre"`
		AlbumType   string `json:"albumType"`
		AlbumID     int64  `json:"albumID"`
	} `json:"basicInfo"`
	Company *struct {
		Name string `json:"name"`
	} `json:"company"`
	Singer *struct {
		SingerList []struct {
			MID  string `json:"mid"`
			Name string `json:"name"`
		} `json:"singerList"`
	} `json:"singer"`
}

type songListData struct {
	TotalNum int `json:"totalNum"`
	SongList []struct {
		SongInfo song `json:"songInfo"`
	} `json:"songList"`
}

// infoEntry 是 get_song_detail 的 info 条目。type 不可靠（作曲与演唱者都可能是
// JUMP_TO_SINGER），角色按 title 判断。
type infoEntry struct {
	Title   string `json:"title"`
	Type    string `json:"type"`
	Content []struct {
		Value string `json:"value"`
		MID   string `json:"mid"`
	} `json:"content"`
}

type songDetailData struct {
	TrackInfo *song       `json:"track_info"`
	Info      []infoEntry `json:"info"`
}

type lyricResponse struct {
	Retcode *int   `json:"retcode"`
	Code    *int   `json:"code"`
	Subcode *int   `json:"subcode"`
	Lyric   string `json:"lyric"`
	Trans   string `json:"trans"`
}
