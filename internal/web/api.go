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

// RegisterAPIRoutes 注册 JSON API 路由
// 将 /api/search 和 /api/download 两个端点注册到给定的路由组中
// api: Gin 路由组，用于注册 API 端点
func RegisterAPIRoutes(api *gin.RouterGroup) {
	// POST /api/search - 音乐搜索接口（JSON格式）
	api.POST("/api/search", handleAPISearch)
	// POST /api/download - 音乐下载接口（JSON请求，数据流响应）
	api.POST("/api/download", handleAPIDownload)
}

// SearchRequest 搜索请求结构体
// 定义了音乐搜索接口的请求参数
type SearchRequest struct {
	// Keyword 搜索关键词，支持歌曲名、歌手名或分享链接
	Keyword string `json:"keyword"`
	// Type 搜索类型，可选值：song(歌曲)、playlist(歌单)、album(专辑)
	Type string `json:"type"`
	// Sources 指定搜索的音乐源，如 netease、qq、kugou 等
	Sources []string `json:"sources"`
	// ExactArtist 精确匹配歌手名，用于过滤搜索结果
	ExactArtist string `json:"exact_artist"`
}

// SongDetail 歌曲详细信息结构体
// 包含歌曲的所有元数据信息，用于 API 响应
type SongDetail struct {
	// ID 歌曲在对应音乐平台的唯一标识符
	ID string `json:"id"`
	// Source 音乐来源平台标识（如 netease、qq、kugou 等）
	Source string `json:"source"`
	// SourceName 音乐来源平台的中文名称（如"网易云音乐"、"QQ音乐"等）
	SourceName string `json:"source_name"`
	// Name 歌曲名称
	Name string `json:"name"`
	// Artist 歌手/艺术家名称
	Artist string `json:"artist"`
	// Album 专辑名称
	Album string `json:"album"`
	// AlbumID 专辑的唯一标识符
	AlbumID string `json:"album_id"`
	// Cover 歌曲封面图片的 URL 地址
	Cover string `json:"cover"`
	// Duration 歌曲时长，单位为秒
	Duration int `json:"duration"`
	// Link 歌曲在原平台的分享链接
	Link string `json:"link"`
	// Ext 音频文件格式扩展名（如 mp3、flac、m4a 等）
	Ext string `json:"ext"`
	// Size 音频文件大小，单位为字节
	Size int64 `json:"size"`
	// Bitrate 音频比特率，单位为 kbps
	Bitrate int `json:"bitrate"`
	// Language 歌曲语言（如华语、英语、日语、韩语等）
	Language string `json:"language"`
	// Genre 歌曲风格/流派分类
	Genre string `json:"genre"`
	// Extra 额外的元数据信息，以键值对形式存储
	Extra map[string]string `json:"extra"`
}

// PlaylistDetail 歌单/专辑详细信息结构体
// 包含歌单或专辑的元数据信息
type PlaylistDetail struct {
	// ID 歌单/专辑在对应音乐平台的唯一标识符
	ID string `json:"id"`
	// Source 音乐来源平台标识
	Source string `json:"source"`
	// SourceName 音乐来源平台的中文名称
	SourceName string `json:"source_name"`
	// Name 歌单/专辑的名称
	Name string `json:"name"`
	// Description 歌单/专辑的描述信息
	Description string `json:"description"`
	// Cover 歌单/专辑封面图片的 URL 地址
	Cover string `json:"cover"`
	// Creator 歌单创建者或专辑歌手名称
	Creator string `json:"creator"`
	// TrackCount 包含的歌曲数量
	TrackCount int `json:"track_count"`
	// Link 歌单/专辑在原平台的分享链接
	Link string `json:"link"`
}

// SearchResponse 搜索响应结构体
// 包含搜索结果或错误信息
type SearchResponse struct {
	// Success 搜索是否成功完成
	Success bool `json:"success"`
	// Keyword 本次搜索使用的关键词
	Keyword string `json:"keyword"`
	// Type 本次搜索的类型（song/playlist/album）
	Type string `json:"type"`
	// Songs 搜索到的歌曲列表
	Songs []SongDetail `json:"songs"`
	// Playlists 搜索到的歌单/专辑列表
	Playlists []PlaylistDetail `json:"playlists"`
	// Error 错误信息，当 success 为 false 时包含具体的错误描述
	Error string `json:"error,omitempty"`
}

// DownloadRequest 下载请求结构体
// 定义了音乐下载接口的请求参数
type DownloadRequest struct {
	// ID 歌曲在对应音乐平台的唯一标识符
	ID string `json:"id"`
	// Source 音乐来源平台标识
	Source string `json:"source"`
	// Name 歌曲名称（用于生成文件名）
	Name string `json:"name"`
	// Artist 歌手名称（用于生成文件名）
	Artist string `json:"artist"`
	// Album 专辑名称（用于嵌入音频元数据）
	Album string `json:"album"`
	// Cover 歌曲封面 URL（用于嵌入音频元数据）
	Cover string `json:"cover"`
	// Extra 额外的歌曲参数信息
	Extra map[string]string `json:"extra"`
}

// handleAPISearch 处理 JSON API 搜索请求
// 支持关键词搜索、链接解析、多源并行搜索
// c: Gin 上下文对象，包含请求和响应信息
func handleAPISearch(c *gin.Context) {
	// 解析 JSON 请求体
	var req SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "error": "invalid request body"})
		return
	}

	// 获取并验证搜索关键词
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		c.JSON(400, gin.H{"success": false, "error": "keyword is required"})
		return
	}

	// 设置默认搜索类型为歌曲
	searchType := strings.TrimSpace(req.Type)
	if searchType == "" {
		searchType = "song"
	}

	// 确定要搜索的音乐源列表
	sources := req.Sources
	if len(sources) == 0 {
		// 如果未指定源，使用该搜索类型的默认源
		sources = defaultSourcesForSearchType(searchType)
	}

	// 初始化结果存储变量
	var allSongs []model.Song
	var allPlaylists []model.Playlist
	var errorMsg string

	// 判断是否为分享链接解析
	if strings.HasPrefix(keyword, "http") {
		// 解析分享链接
		src := core.DetectSource(keyword)
		if src == "" {
			errorMsg = "不支持该链接的解析，或无法识别来源"
		} else {
			// 尝试解析歌曲链接
			parsed := false
			parseFn := core.GetParseFunc(src)
			if parseFn != nil {
				if song, err := parseFn(keyword); err == nil {
					allSongs = append(allSongs, *song)
					searchType = "song"
					parsed = true
				}
			}
			// 尝试解析歌单链接
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
			// 尝试解析专辑链接
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
			// 如果所有解析都失败，返回错误信息
			if !parsed {
				errorMsg = fmt.Sprintf("解析失败: 暂不支持 %s 平台的此链接类型或解析出错", src)
			}
		}
	} else {
		// 关键词搜索，使用多源并行搜索
		var wg sync.WaitGroup
		var mu sync.Mutex

		// 遍历所有指定的音乐源进行并行搜索
		for _, src := range sources {
			wg.Add(1)
			go func(s string) {
				defer wg.Done()
				if searchType == "playlist" {
					// 搜索歌单
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
					// 搜索专辑
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
					// 搜索歌曲
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

	// 如果指定了精确歌手过滤，对歌曲结果进行过滤
	if searchType == "song" && req.ExactArtist != "" && len(allSongs) > 0 {
		allSongs = filterSongsByExactArtist(allSongs, req.ExactArtist)
	}

	// 将歌曲模型转换为详细信息结构体
	songDetails := make([]SongDetail, 0, len(allSongs))
	for _, song := range allSongs {
		songDetails = append(songDetails, songToDetail(song))
	}

	// 将歌单/专辑模型转换为详细信息结构体
	playlistDetails := make([]PlaylistDetail, 0, len(allPlaylists))
	for _, playlist := range allPlaylists {
		playlistDetails = append(playlistDetails, playlistToDetail(playlist))
	}

	// 返回 JSON 响应
	c.JSON(200, SearchResponse{
		Success:   errorMsg == "",
		Keyword:   keyword,
		Type:      searchType,
		Songs:     songDetails,
		Playlists: playlistDetails,
		Error:     errorMsg,
	})
}

// songToDetail 将歌曲模型转换为歌曲详细信息结构体
// 提取并整理歌曲的所有元数据信息
// song: 原始歌曲模型对象
// 返回: 包含详细信息的 SongDetail 结构体
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

// playlistToDetail 将歌单/专辑模型转换为详细信息结构体
// 提取并整理歌单/专辑的所有元数据信息
// playlist: 原始歌单/专辑模型对象
// 返回: 包含详细信息的 PlaylistDetail 结构体
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

// getLanguageFromExtra 从额外参数中提取歌曲语言信息
// extra: 歌曲的额外参数映射表
// 返回: 语言字符串，如果未找到则返回空字符串
func getLanguageFromExtra(extra map[string]string) string {
	if extra == nil {
		return ""
	}
	// 尝试多种可能的键名
	if lang, ok := extra["language"]; ok {
		return lang
	}
	if lang, ok := extra["lang"]; ok {
		return lang
	}
	return ""
}

// getGenreFromExtra 从额外参数中提取歌曲风格/流派信息
// extra: 歌曲的额外参数映射表
// 返回: 风格字符串，如果未找到则返回空字符串
func getGenreFromExtra(extra map[string]string) string {
	if extra == nil {
		return ""
	}
	// 尝试多种可能的键名
	if genre, ok := extra["genre"]; ok {
		return genre
	}
	if category, ok := extra["category"]; ok {
		return category
	}
	return ""
}

// handleAPIDownload 处理 JSON API 下载请求
// 支持从各音乐平台下载歌曲，返回音频数据流
// c: Gin 上下文对象，包含请求和响应信息
func handleAPIDownload(c *gin.Context) {
	// 解析 JSON 请求体
	var req DownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "error": "invalid request body"})
		return
	}

	// 获取并验证必要参数
	id := strings.TrimSpace(req.ID)
	source := strings.TrimSpace(req.Source)
	name := strings.TrimSpace(req.Name)
	artist := strings.TrimSpace(req.Artist)
	album := strings.TrimSpace(req.Album)
	coverURL := strings.TrimSpace(req.Cover)

	// 验证 ID 和来源是否为必填项
	if id == "" || source == "" {
		c.JSON(400, gin.H{"success": false, "error": "id and source are required"})
		return
	}

	// 设置默认值
	if name == "" {
		name = "Unknown"
	}
	if artist == "" {
		artist = "Unknown"
	}

	// 处理本地音乐源
	if isLocalMusicSource(source) {
		serveLocalMusicDownload(c, id, false)
		return
	}

	// 获取 Web 设置
	settings := core.GetWebSettings()

	// 构建歌曲对象
	tempSong := &model.Song{
		ID:     id,
		Source: source,
		Name:   name,
		Artist: artist,
		Album:  album,
		Cover:  coverURL,
		Extra:  req.Extra,
	}

	// 处理汽水音乐特殊下载逻辑
	if source == "soda" {
		cookie := core.CM.Get("soda")
		sodaInst := soda.New(cookie)
		info, err := sodaInst.GetDownloadInfo(tempSong)
		if err != nil {
			c.JSON(502, gin.H{"success": false, "error": "Soda info error"})
			return
		}
		httpReq, err := core.BuildSourceRequest("GET", info.URL, "soda", "")
		if err != nil {
			c.JSON(502, gin.H{"success": false, "error": "Soda request error"})
			return
		}
		client := &http.Client{}
		resp, err := client.Do(httpReq)
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

	// 获取对应音乐源的下载函数
	dlFunc := core.GetDownloadFunc(source)
	if dlFunc == nil {
		c.JSON(400, gin.H{"success": false, "error": "Unknown source"})
		return
	}

	// 获取下载 URL
	downloadUrl, err := dlFunc(tempSong)
	if err != nil {
		c.JSON(404, gin.H{"success": false, "error": "Failed to get URL"})
		return
	}

	// 构建 HTTP 请求，支持 Range 请求
	httpReq, err := core.BuildSourceRequest("GET", downloadUrl, source, c.GetHeader("Range"))
	if err != nil {
		c.JSON(502, gin.H{"success": false, "error": "Upstream request error"})
		return
	}

	// 发起请求获取音频数据
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(502, gin.H{"success": false, "error": "Upstream stream error"})
		return
	}
	defer resp.Body.Close()

	// 复制响应头信息（过滤掉不需要的头部）
	for k, v := range resp.Header {
		if k != "Transfer-Encoding" && k != "Date" && k != "Access-Control-Allow-Origin" {
			c.Writer.Header()[k] = v
		}
	}

	// 检测音频文件扩展名
	ext := core.DetectAudioExtByContentType(resp.Header.Get("Content-Type"))
	if ext == "" {
		// 尝试从 URL 路径中提取扩展名
		if parsedURL, parseErr := url.Parse(downloadUrl); parseErr == nil {
			suffix := strings.ToLower(strings.TrimPrefix(path.Ext(parsedURL.Path), "."))
			switch suffix {
			case "mp3", "flac", "ogg", "m4a":
				ext = suffix
			}
		}
	}
	if ext == "" {
		// 尝试从歌曲信息中获取扩展名
		ext = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tempSong.Ext, ".")))
	}
	if ext == "" {
		// 默认使用 MP3 格式
		ext = "mp3"
	}

	// 构建下载文件名并设置响应头
	filename := core.BuildDownloadFilename(tempSong, ext, settings.DownloadFilenameTemplate)
	setDownloadHeader(c, filename)
	c.Status(resp.StatusCode)
	// 流式传输音频数据
	io.Copy(c.Writer, resp.Body)
}
