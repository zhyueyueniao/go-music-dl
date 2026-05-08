package web

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/guohuiyuan/go-music-dl/core"
	"github.com/guohuiyuan/music-lib/model"
	"github.com/guohuiyuan/music-lib/soda"
)

func RegisterAPIRoutes(api *gin.RouterGroup) {
	api.POST("/api/search", handleAPISearch)
	api.POST("/api/download", handleAPIDownload)
}

type SearchRequest struct {
	Keyword   string   `json:"keyword"`
	Type      string   `json:"type"`
	Sources   []string `json:"sources"`
	ExactArtist string `json:"exact_artist"`
}

type SongDetail struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	SourceName string            `json:"source_name"`
	Name       string            `json:"name"`
	Artist     string            `json:"artist"`
	Album      string            `json:"album"`
	AlbumID    string            `json:"album_id"`
	Cover      string            `json:"cover"`
	Duration   int               `json:"duration"`
	Link       string            `json:"link"`
	Ext        string            `json:"ext"`
	Size       int64             `json:"size"`
	Bitrate    int               `json:"bitrate"`
	Language   string            `json:"language"`
	Genre      string            `json:"genre"`
	Extra      map[string]string `json:"extra"`
}

type PlaylistDetail struct {
	ID          string         `json:"id"`
	Source      string         `json:"source"`
	SourceName  string         `json:"source_name"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Cover       string         `json:"cover"`
	Creator     string         `json:"creator"`
	TrackCount  int            `json:"track_count"`
	Link        string         `json:"link"`
}

type SearchResponse struct {
	Success   bool              `json:"success"`
	Keyword   string            `json:"keyword"`
	Type      string            `json:"type"`
	Songs     []SongDetail      `json:"songs"`
	Playlists []PlaylistDetail  `json:"playlists"`
	Error     string            `json:"error,omitempty"`
}

type DownloadRequest struct {
	ID     string            `json:"id"`
	Source string            `json:"source"`
	Name   string            `json:"name"`
	Artist string            `json:"artist"`
	Album  string            `json:"album"`
	Cover  string            `json:"cover"`
	Extra  map[string]string `json:"extra"`
}

func handleAPISearch(c *gin.Context) {
	var req SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "error": "invalid request body"})
		return
	}

	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		c.JSON(400, gin.H{"success": false, "error": "keyword is required"})
		return
	}

	searchType := strings.TrimSpace(req.Type)
	if searchType == "" {
		searchType = "song"
	}

	sources := req.Sources
	if len(sources) == 0 {
		sources = defaultSourcesForSearchType(searchType)
	}

	var allSongs []model.Song
	var allPlaylists []model.Playlist
	var errorMsg string

	if strings.HasPrefix(keyword, "http") {
		src := core.DetectSource(keyword)
		if src == "" {
			errorMsg = "不支持该链接的解析，或无法识别来源"
		} else {
			parsed := false
			parseFn := core.GetParseFunc(src)
			if parseFn != nil {
				if song, err := parseFn(keyword); err == nil {
					allSongs = append(allSongs, *song)
					searchType = "song"
					parsed = true
				}
			}
			if !parsed {
				parsePlaylistFn := core.GetParsePlaylistFunc(src)
				if parsePlaylistFn != nil {
					if playlist, songs, err := parsePlaylistFn(keyword); err == nil {
						if searchType == "playlist" {
							allPlaylists = append(allPlaylists, *playlist)
						} else {
							allSongs = append(allSongs, songs...)
							searchType = "song"
						}
						parsed = true
					}
				}
			}
			if !parsed {
				parseAlbumFn := core.GetParseAlbumFunc(src)
				if parseAlbumFn != nil {
					if album, songs, err := parseAlbumFn(keyword); err == nil {
						if searchType == "album" {
							allPlaylists = append(allPlaylists, *album)
						} else {
							allSongs = append(allSongs, songs...)
							searchType = "song"
						}
						parsed = true
					}
				}
			}
			if !parsed {
				errorMsg = fmt.Sprintf("解析失败: 暂不支持 %s 平台的此链接类型或解析出错", src)
			}
		}
	} else {
		var wg sync.WaitGroup
		var mu sync.Mutex

		for _, src := range sources {
			wg.Add(1)
			go func(s string) {
				defer wg.Done()
				if searchType == "playlist" {
					fn := core.GetPlaylistSearchFunc(s)
					if fn != nil {
						res, err := fn(keyword)
						if err == nil {
							for i := range res {
								res[i].Source = s
							}
							mu.Lock()
							allPlaylists = append(allPlaylists, res...)
							mu.Unlock()
						}
					}
				} else if searchType == "album" {
					fn := core.GetAlbumSearchFunc(s)
					if fn != nil {
						res, err := fn(keyword)
						if err == nil {
							for i := range res {
								res[i].Source = s
							}
							mu.Lock()
							allPlaylists = append(allPlaylists, res...)
							mu.Unlock()
						}
					}
				} else {
					fn := core.GetSearchFunc(s)
					if fn != nil {
						res, err := fn(keyword)
						if err == nil {
							for i := range res {
								res[i].Source = s
							}
							mu.Lock()
							allSongs = append(allSongs, res...)
							mu.Unlock()
						}
					}
				}
			}(src)
		}
		wg.Wait()
	}

	if searchType == "song" && req.ExactArtist != "" && len(allSongs) > 0 {
		allSongs = filterSongsByExactArtist(allSongs, req.ExactArtist)
	}

	songDetails := make([]SongDetail, 0, len(allSongs))
	for _, song := range allSongs {
		songDetails = append(songDetails, songToDetail(song))
	}

	playlistDetails := make([]PlaylistDetail, 0, len(allPlaylists))
	for _, playlist := range allPlaylists {
		playlistDetails = append(playlistDetails, playlistToDetail(playlist))
	}

	c.JSON(200, SearchResponse{
		Success:   errorMsg == "",
		Keyword:   keyword,
		Type:      searchType,
		Songs:     songDetails,
		Playlists: playlistDetails,
		Error:     errorMsg,
	})
}

func songToDetail(song model.Song) SongDetail {
	return SongDetail{
		ID:         song.ID,
		Source:     song.Source,
		SourceName: core.GetSourceDescription(song.Source),
		Name:       song.Name,
		Artist:     song.Artist,
		Album:      song.Album,
		AlbumID:    song.AlbumID,
		Cover:      song.Cover,
		Duration:   song.Duration,
		Link:       song.Link,
		Ext:        song.Ext,
		Size:       song.Size,
		Bitrate:    song.Bitrate,
		Language:   getLanguageFromExtra(song.Extra),
		Genre:      getGenreFromExtra(song.Extra),
		Extra:      song.Extra,
	}
}

func playlistToDetail(playlist model.Playlist) PlaylistDetail {
	return PlaylistDetail{
		ID:          playlist.ID,
		Source:      playlist.Source,
		SourceName:  core.GetSourceDescription(playlist.Source),
		Name:        playlist.Name,
		Description: playlist.Description,
		Cover:       playlist.Cover,
		Creator:     playlist.Creator,
		TrackCount:  playlist.TrackCount,
		Link:        playlist.Link,
	}
}

func getLanguageFromExtra(extra map[string]string) string {
	if extra == nil {
		return ""
	}
	if lang, ok := extra["language"]; ok {
		return lang
	}
	if lang, ok := extra["lang"]; ok {
		return lang
	}
	return ""
}

func getGenreFromExtra(extra map[string]string) string {
	if extra == nil {
		return ""
	}
	if genre, ok := extra["genre"]; ok {
		return genre
	}
	if category, ok := extra["category"]; ok {
		return category
	}
	return ""
}

func handleAPIDownload(c *gin.Context) {
	var req DownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "error": "invalid request body"})
		return
	}

	id := strings.TrimSpace(req.ID)
	source := strings.TrimSpace(req.Source)
	name := strings.TrimSpace(req.Name)
	artist := strings.TrimSpace(req.Artist)
	album := strings.TrimSpace(req.Album)
	coverURL := strings.TrimSpace(req.Cover)

	if id == "" || source == "" {
		c.JSON(400, gin.H{"success": false, "error": "id and source are required"})
		return
	}
	if name == "" {
		name = "Unknown"
	}
	if artist == "" {
		artist = "Unknown"
	}

	if isLocalMusicSource(source) {
		serveLocalMusicDownload(c, id, false)
		return
	}

	settings := core.GetWebSettings()
	tempSong := &model.Song{
		ID:     id,
		Source: source,
		Name:   name,
		Artist: artist,
		Album:  album,
		Cover:  coverURL,
		Extra:  req.Extra,
	}

	if source == "soda" {
		cookie := core.CM.Get("soda")
		sodaInst := soda.New(cookie)
		info, err := sodaInst.GetDownloadInfo(tempSong)
		if err != nil {
			c.JSON(502, gin.H{"success": false, "error": "Soda info error"})
			return
		}
		req, reqErr := core.BuildSourceRequest("GET", info.URL, "soda", "")
		if reqErr != nil {
			c.JSON(502, gin.H{"success": false, "error": "Soda request error"})
			return
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(502, gin.H{"success": false, "error": "Soda stream error"})
			return
		}
		defer resp.Body.Close()
		encryptedData, _ := io.ReadAll(resp.Body)
		finalData, err := soda.DecryptAudio(encryptedData, info.PlayAuth)
		if err != nil {
			c.JSON(500, gin.H{"success": false, "error": "Decrypt failed"})
			return
		}
		ext := core.DetectAudioExt(finalData)
		filename := core.BuildDownloadFilename(tempSong, ext, settings.DownloadFilenameTemplate)
		setDownloadHeader(c, filename)
		c.Data(200, core.AudioMimeByExt(ext), finalData)
		return
	}

	dlFunc := core.GetDownloadFunc(source)
	if dlFunc == nil {
		c.JSON(400, gin.H{"success": false, "error": "Unknown source"})
		return
	}

	downloadUrl, err := dlFunc(tempSong)
	if err != nil {
		c.JSON(404, gin.H{"success": false, "error": "Failed to get URL"})
		return
	}

	httpReq, err := core.BuildSourceRequest("GET", downloadUrl, source, c.GetHeader("Range"))
	if err != nil {
		c.JSON(502, gin.H{"success": false, "error": "Upstream request error"})
		return
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(502, gin.H{"success": false, "error": "Upstream stream error"})
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		if k != "Transfer-Encoding" && k != "Date" && k != "Access-Control-Allow-Origin" {
			c.Writer.Header()[k] = v
		}
	}

	ext := core.DetectAudioExtByContentType(resp.Header.Get("Content-Type"))
	if ext == "" {
		if parsedURL, parseErr := url.Parse(downloadUrl); parseErr == nil {
			suffix := strings.ToLower(strings.TrimPrefix(path.Ext(parsedURL.Path), "."))
			switch suffix {
			case "mp3", "flac", "ogg", "m4a":
				ext = suffix
			}
		}
	}
	if ext == "" {
		ext = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tempSong.Ext, ".")))
	}
	if ext == "" {
		ext = "mp3"
	}

	filename := core.BuildDownloadFilename(tempSong, ext, settings.DownloadFilenameTemplate)
	setDownloadHeader(c, filename)
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}