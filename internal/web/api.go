package web

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/guohuiyuan/go-music-dl/core"
	"github.com/guohuiyuan/music-lib/model"
	"github.com/guohuiyuan/music-lib/soda"
)

// apiKeyFromEnv 从环境变量获取 API Key
// 环境变量名: MUSIC_DL_API_KEY
// 如果未设置或为空，则不启用 API Key 认证
var apiKeyFromEnv = strings.TrimSpace(os.Getenv("MUSIC_DL_API_KEY"))

// apiKeyMiddleware API Key 认证中间件
// 检查请求头中的 X-API-Key 是否与配置的 API Key 匹配
// 如果环境变量 MUSIC_DL_API_KEY 未设置，则跳过认证
func apiKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果环境变量未设置 API Key，跳过认证
		if apiKeyFromEnv == "" {
			c.Next()
			return
		}

		// 从请求头获取 API Key
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "API Key is required",
			})
			c.Abort()
			return
		}

		// 验证 API Key
		if apiKey != apiKeyFromEnv {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "Invalid API Key",
			})
			c.Abort()
			return
		}

		// 认证通过，继续处理请求
		c.Next()
	}
}

// RegisterAPIRoutes 注册 JSON API 路由
// 将 /api/search 和 /api/download 两个端点注册到给定的路由组中
// api: Gin 路由组，用于注册 API 端点
func RegisterAPIRoutes(api *gin.RouterGroup) {
	// 创建 API 子路由组，应用 API Key 认证中间件
	apiGroup := api.Group("/api")
	apiGroup.Use(apiKeyMiddleware())

	// POST /api/search - 音乐搜索接口（JSON格式）
	apiGroup.POST("/search", handleAPISearch)
	// POST /api/download - 音乐下载接口（JSON请求，数据流响应）
	apiGroup.POST("/download", handleAPIDownload)
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
	// IncludeLocal 是否包含本地音乐搜索，默认为 true
	IncludeLocal bool `json:"include_local"`
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
	// HasMetadata 歌曲是否有完整的内置元数据（仅本地歌曲）
	HasMetadata bool `json:"has_metadata,omitempty"`
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
// 只需提供 id 和 source，其他元数据将从搜索结果中获取
type DownloadRequest struct {
	// ID 歌曲在对应音乐平台的唯一标识符
	ID string `json:"id" binding:"required"`
	// Source 音乐来源平台标识（如 netease、qq、kugou 等）
	Source string `json:"source" binding:"required"`
}

// handleAPISearch 处理 JSON API 搜索请求
// 支持关键词搜索、链接解析、多源并行搜索，同时支持搜索本地音乐
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

		// 如果启用了本地音乐搜索或未指定源，默认包含本地音乐
		if req.IncludeLocal || len(req.Sources) == 0 {
			// 搜索本地音乐
			localTracks, dir, exists, err := scanLocalMusicTracks()
			if err == nil && exists && len(localTracks) > 0 {
				// 在本地音乐中搜索匹配的歌曲
				keywordLower := strings.ToLower(keyword)
				for _, track := range localTracks {
					// 通过歌曲名称匹配
					if strings.Contains(strings.ToLower(track.Name), keywordLower) {
						localSong := modelToLocalSongWithMetadata(track)
						// 歌手匹配过滤
						if req.ExactArtist == "" || strings.ToLower(track.Artist) == strings.ToLower(req.ExactArtist) {
							mu.Lock()
							allSongs = append(allSongs, localSong)
							mu.Unlock()
						}
					}
					// 通过歌手名称匹配
					if req.ExactArtist == "" && strings.Contains(strings.ToLower(track.Artist), keywordLower) {
						localSong := modelToLocalSongWithMetadata(track)
						// 检查是否已经添加过（避免重复）
						alreadyExists := false
						for _, existing := range allSongs {
							if existing.ID == localSong.ID && existing.Source == localSong.Source {
								alreadyExists = true
								break
							}
						}
						if !alreadyExists {
							mu.Lock()
							allSongs = append(allSongs, localSong)
							mu.Unlock()
						}
					}
					_ = dir // 避免未使用变量警告
				}
			}
		}
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
	detail := SongDetail{
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

	// 检查是否有完整元数据标记
	if song.Extra != nil {
		if hasMeta, ok := song.Extra["has_metadata"]; ok && hasMeta == "true" {
			detail.HasMetadata = true
		}
	}

	return detail
}

// checkLocalTrackHasMetadata 检查本地歌曲是否有完整的内置元数据
// 通过检查 track.Missing 是否为空来判断
func checkLocalTrackHasMetadata(track *localMusicTrack) bool {
	return track != nil && (len(track.Missing) == 0 || (len(track.Missing) == 1 && track.Missing[0] == ""))
}

// modelToLocalSongWithMetadata 将本地歌曲 track 转换为 model.Song
// 自动检测并标记是否有完整的内置元数据
func modelToLocalSongWithMetadata(track *localMusicTrack) model.Song {
	song := model.Song{
		ID:       track.ID,
		Source:   localMusicSource,
		Name:     track.Name,
		Artist:   track.Artist,
		Album:    track.Album,
		Cover:    track.Cover,
		Duration: track.Duration,
		Extra:    track.Extra,
	}

	// 检查并标记是否有完整元数据
	if checkLocalTrackHasMetadata(track) {
		if song.Extra == nil {
			song.Extra = make(map[string]string)
		}
		song.Extra["has_metadata"] = "true"
	}

	return song
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
// 支持两种模式：
// 1. 在线下载：从各音乐平台下载歌曲，自动嵌入完整元数据后保存到本地，最后读取本地文件返回
// 2. 本地读取：检查本地歌曲是否有完整元数据，有则直接返回，无则尝试获取元数据并写入后再返回
// 自动嵌入歌曲完整元数据（标题、艺术家、专辑、封面、歌词）到音频文件
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

	// 验证 ID 和来源是否为必填项
	if id == "" || source == "" {
		c.JSON(400, gin.H{"success": false, "error": "id and source are required"})
		return
	}

	// 处理本地音乐源
	if isLocalMusicSource(source) {
		// 检查本地歌曲是否有完整元数据
		track, err := localMusicTrackByID(id)
		if err != nil {
			c.JSON(404, gin.H{"success": false, "error": "Local music not found"})
			return
		}

		// 检查是否有完整元数据
		if checkLocalTrackHasMetadata(track) {
			// 有完整元数据，直接返回
			serveLocalMusicDownload(c, id, false)
			return
		}

		// 元数据不完整，尝试从在线获取并写入
		// 通过原始文件 ID 获取在线信息
		onlineSong, err := getOnlineSongByLocalTrack(track)
		if err != nil {
			// 无法获取在线信息，直接返回本地文件
			serveLocalMusicDownload(c, id, false)
			return
		}

		// 下载在线版本并嵌入元数据
		onlineSong.Source = source
		downloadAndSaveWithMetadata(onlineSong, c)
		return
	}

	// 在线下载模式
	// 构建歌曲对象
	tempSong := &model.Song{
		ID:     id,
		Source: source,
	}

	// 获取 Web 设置
	settings := core.GetWebSettings()

	var audioData []byte
	var ext string

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
		audioData, err = soda.DecryptAudio(encryptedData, info.PlayAuth)
		if err != nil {
			c.JSON(500, gin.H{"success": false, "error": "Decrypt failed"})
			return
		}
		ext = core.DetectAudioExt(audioData)
	} else {
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

		// 构建 HTTP 请求
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

		// 读取所有音频数据
		audioData, err = io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(502, gin.H{"success": false, "error": "Failed to read audio data"})
			return
		}

		// 检测音频文件扩展名
		ext = core.DetectAudioExtByContentType(resp.Header.Get("Content-Type"))
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
	}

	// 构建下载文件名
	filename := core.BuildDownloadFilename(tempSong, ext, settings.DownloadFilenameTemplate)

	// 保存到本地音乐目录并嵌入元数据
	savedPath, err := saveToLocalMusicDir(audioData, ext, tempSong, filename)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "error": fmt.Sprintf("save to local failed: %v", err)})
		return
	}

	// 读取本地文件并返回
	serveSavedFile(c, savedPath, filename, ext)
}

// getOnlineSongByLocalTrack 根据本地歌曲信息尝试获取在线歌曲信息
// 通过文件名中的歌名和歌手尝试搜索
func getOnlineSongByLocalTrack(track *localMusicTrack) (*model.Song, error) {
	if track == nil {
		return nil, fmt.Errorf("track is nil")
	}

	songName := strings.TrimSpace(track.Name)
	if songName == "" || songName == "Unknown" {
		return nil, fmt.Errorf("no song name available")
	}

	// 尝试搜索歌曲
	var songs []model.Song
	var wg sync.WaitGroup
	var mu sync.Mutex
	var searchErr error

	// 使用默认搜索源
	sources := core.GetDefaultSourceNames()
	for _, src := range sources {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			fn := core.GetSearchFunc(s)
			if fn == nil {
				return
			}
			res, err := fn(songName)
			if err != nil {
				mu.Lock()
				if searchErr == nil {
					searchErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			for i := range res {
				res[i].Source = s
			}
			songs = append(songs, res...)
			mu.Unlock()
		}(src)
	}
	wg.Wait()

	if len(songs) == 0 {
		if searchErr != nil {
			return nil, searchErr
		}
		return nil, fmt.Errorf("no search results")
	}

	// 尝试匹配歌手
	artistName := strings.TrimSpace(track.Artist)
	if artistName != "" && artistName != "未知歌手" {
		artistLower := strings.ToLower(artistName)
		for _, song := range songs {
			if strings.Contains(strings.ToLower(song.Artist), artistLower) {
				return &song, nil
			}
		}
	}

	// 返回第一个结果
	return &songs[0], nil
}

// downloadAndSaveWithMetadata 下载歌曲、嵌入元数据并返回
func downloadAndSaveWithMetadata(song *model.Song, c *gin.Context) {
	if song == nil || song.ID == "" || song.Source == "" {
		c.JSON(400, gin.H{"success": false, "error": "invalid song info"})
		return
	}

	// 获取 Web 设置
	settings := core.GetWebSettings()

	var audioData []byte
	var ext string

	// 获取音频数据
	dlFunc := core.GetDownloadFunc(song.Source)
	if dlFunc == nil {
		c.JSON(400, gin.H{"success": false, "error": "Unknown source"})
		return
	}

	downloadUrl, err := dlFunc(song)
	if err != nil {
		c.JSON(404, gin.H{"success": false, "error": "Failed to get URL"})
		return
	}

	httpReq, err := core.BuildSourceRequest("GET", downloadUrl, song.Source, "")
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

	audioData, err = io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(502, gin.H{"success": false, "error": "Failed to read audio data"})
		return
	}

	// 检测扩展名
	ext = core.DetectAudioExtByContentType(resp.Header.Get("Content-Type"))
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
		ext = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(song.Ext, ".")))
	}
	if ext == "" {
		ext = "mp3"
	}

	// 构建文件名
	filename := core.BuildDownloadFilename(song, ext, settings.DownloadFilenameTemplate)

	// 保存并嵌入元数据
	savedPath, err := saveToLocalMusicDir(audioData, ext, song, filename)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "error": fmt.Sprintf("save to local failed: %v", err)})
		return
	}

	// 返回文件
	serveSavedFile(c, savedPath, filename, ext)
}

// serveSavedFile 读取本地文件并返回给客户端
func serveSavedFile(c *gin.Context, filePath string, filename string, ext string) {
	file, err := os.Open(filePath)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "error": "Failed to read saved file"})
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		c.JSON(500, gin.H{"success": false, "error": "Failed to get file info"})
		return
	}

	c.Header("Content-Type", core.AudioMimeByExt(ext))
	c.Header("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	setDownloadHeader(c, filename)
	http.ServeContent(c.Writer, c.Request, filename, fileInfo.ModTime(), file)
}

// saveToLocalMusicDir 将音频数据保存到本地音乐目录并嵌入元数据
// audioData: 原始音频数据
// ext: 音频文件扩展名
// song: 歌曲信息对象，用于获取元数据
// filenameHint: 文件名提示
// 返回保存后的文件路径和错误信息
func saveToLocalMusicDir(audioData []byte, ext string, song *model.Song, filenameHint string) (string, error) {
	// 获取本地音乐目录
	dir := localMusicDownloadDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// 获取绝对路径
	rootAbs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// 确保文件名合法
	safeFilename := strings.TrimSpace(filenameHint)
	if safeFilename == "" {
		safeFilename = fmt.Sprintf("%s - %s.%s", song.Name, song.Artist, ext)
	}
	// 清理文件名中的非法字符
	safeFilename = sanitizeFilenameForSave(safeFilename)

	// 生成唯一文件路径
	filePath := uniqueLocalMusicPath(rootAbs, safeFilename)

	// 获取歌词（如果可用）
	var lyric string
	if lyricFn := core.GetLyricFunc(song.Source); lyricFn != nil {
		lyric, _ = lyricFn(song)
	}

	// 获取封面数据
	var coverData []byte
	var coverMime string
	if strings.TrimSpace(song.Cover) != "" {
		coverData, coverMime, _ = core.FetchBytesWithMime(song.Cover, song.Source)
	}

	// 嵌入元数据到音频文件
	embeddedData, err := core.EmbedSongMetadata(audioData, song, lyric, coverData, coverMime)
	if err != nil {
		// 如果嵌入失败，使用原始数据
		embeddedData = audioData
	}

	// 写入文件
	if err := os.WriteFile(filePath, embeddedData, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return filePath, nil
}

// sanitizeFilenameForSave 清理文件名中的非法字符
// filename: 原始文件名
// 返回清理后的安全文件名
func sanitizeFilenameForSave(filename string) string {
	// 替换 Windows 和通用文件系统中的非法字符
	illegalChars := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}
	result := filename
	for _, char := range illegalChars {
		result = strings.ReplaceAll(result, char, "_")
	}
	// 去除首尾空格和点
	result = strings.Trim(result, " .")
	// 确保文件名不为空
	if result == "" {
		result = "music"
	}
	return result
}
