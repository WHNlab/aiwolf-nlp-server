package transport

import (
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aiwolfdial/aiwolf-nlp-server/room"
	"github.com/aiwolfdial/aiwolf-nlp-server/web"
	"github.com/gin-gonic/gin"
)

const sessionCookieName = "aiwolf_session"

// buildWebRouter は人間向けWeb UI・ルームAPIを配信するルータ。
// AI用の /ws はここに置かず、web_socket ポート側のルータだけが持つ。
func (s *Server) buildWebRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	staticFS, err := fs.Sub(web.Static, "static")
	if err == nil {
		router.GET("/", func(c *gin.Context) { s.serveStatic(c, staticFS, "index.html") })
		router.GET("/app", func(c *gin.Context) { s.serveStatic(c, staticFS, "index.html") })
		router.GET("/rooms/new", func(c *gin.Context) { s.serveStatic(c, staticFS, "index.html") })
		router.GET("/rooms/:id", func(c *gin.Context) { s.serveStatic(c, staticFS, "index.html") })
		router.GET("/static/*path", func(c *gin.Context) {
			s.serveStatic(c, staticFS, strings.TrimPrefix(c.Param("path"), "/"))
		})
	}

	api := router.Group("/api/v1")
	api.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": Version.Version})
	})
	api.GET("/room-presets", s.handleRoomPresets)
	// トップページからはバージョンに依存しないURLでも同じ参加キットを配布する。
	servePlayerKit := func(c *gin.Context) {
		data, err := web.PlayerKitArchive()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "参加キットを生成できませんでした"})
			return
		}
		c.Header("Content-Disposition", `attachment; filename="aiwolf-player-`+web.PlayerKitVersion+`.zip"`)
		c.Data(http.StatusOK, "application/zip", data)
	}
	router.GET(web.PlayerKitPath, servePlayerKit)
	router.GET("/downloads/aiwolf-player.zip", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		servePlayerKit(c)
	})
	router.GET("/agent/SKILL.md", func(c *gin.Context) {
		data, _ := web.PlayerKit.ReadFile("kit/aiwolf-player/SKILL.md")
		c.Data(http.StatusOK, "text/plain; charset=utf-8", data)
	})
	router.GET("/agent/GUIDE.md", func(c *gin.Context) {
		data, _ := web.PlayerKit.ReadFile("kit/aiwolf-player/GUIDE.md")
		c.Data(http.StatusOK, "text/plain; charset=utf-8", data)
	})

	rooms := api.Group("/rooms")
	rooms.GET("", s.handleListRooms)
	rooms.POST("", s.handleCreateRoom)
	rooms.POST("/:id/join", s.checkOrigin(), s.handleJoinRoom)
	rooms.POST("/:id/leave", s.checkOrigin(), s.handleLeaveRoom)
	rooms.POST("/:id/close", s.checkOrigin(), s.handleCloseRoom)
	rooms.POST("/:id/start", s.checkOrigin(), s.handleStartRoom)
	rooms.POST("/:id/claim", s.checkOrigin(), s.handleClaimSeat)
	rooms.PUT("/:id/rental", s.checkOrigin(), s.handleUpdateRental)
	rooms.POST("/:id/consultations", s.checkOrigin(), s.handleSendAdvice)
	rooms.GET("/:id/consultations", s.handleListConsultations)
	rooms.GET("/:id/invite", s.handleAgentInvite)
	rooms.GET("/:id", s.handleGetRoom)
	rooms.GET("/:id/history", s.handleRoomHistory)
	rooms.GET("/:id/events", s.handleRoomEvents)
	api.GET("/rental-capabilities", s.handleRentalCapabilities)
	return router
}

// serveStatic はSPAの静的ファイルを返す。見つからなければindex.htmlにフォールバックしない
// （API/静的以外のパスは404）。
func (s *Server) serveStatic(c *gin.Context, fsys fs.FS, path string) {
	if path == "" || path == "/" {
		path = "index.html"
	}
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	// Content-Typeは拡張子で決める。embedの自動判定は未対応。
	switch {
	case strings.HasSuffix(path, ".html"):
		// HTMLは毎回確認させ、更新時には新しいCSS/JSのURLを参照させる。
		c.Header("Cache-Control", "no-store")
		data = []byte(strings.ReplaceAll(string(data), "__ASSET_VERSION__", web.AssetVersion))
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	case strings.HasSuffix(path, ".css"):
		c.Data(http.StatusOK, "text/css; charset=utf-8", data)
	case strings.HasSuffix(path, ".js"):
		c.Header("Cache-Control", "no-cache")
		data = []byte(strings.ReplaceAll(string(data), "__ASSET_VERSION__", web.AssetVersion))
		c.Data(http.StatusOK, "text/javascript; charset=utf-8", data)
	case strings.HasSuffix(path, ".svg"):
		c.Data(http.StatusOK, "image/svg+xml", data)
	case strings.HasSuffix(path, ".png"):
		c.Data(http.StatusOK, "image/png", data)
	case strings.HasSuffix(path, ".webmanifest"):
		c.Data(http.StatusOK, "application/manifest+json", data)
	default:
		c.Data(http.StatusOK, "application/octet-stream", data)
	}
}

// checkOrigin は同一オリジン確認を行う。Webの更新APIだけに適用する。
func (s *Server) checkOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		// OriginのauthorityとリクエストHostが一致することだけを認める。
		// 部分一致だと evil.example.com:8080 のような別オリジンを通してしまう。
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, c.Request.Host) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "origin mismatch"})
			return
		}
		c.Next()
	}
}

// ---- セッション ----

func (s *Server) session(c *gin.Context) (*room.Session, bool) {
	if s.roomManager == nil {
		return nil, false
	}
	token, _ := c.Cookie(sessionCookieName)
	sess, fresh := s.roomManager.GetOrCreateSession(token)
	if fresh {
		// 発行済みなら毎回Cookieを書き直す。SSEの初回接続にも乗る。
		s.setSessionCookie(c, sess)
	}
	return sess, fresh
}

func (s *Server) setSessionCookie(c *gin.Context, sess *room.Session) {
	secure := c.Request.TLS != nil
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) roomParam(c *gin.Context) *room.Room {
	if s.roomManager == nil {
		return nil
	}
	return s.roomManager.GetRoom(c.Param("id"))
}

func jsonError(c *gin.Context, err error) {
	code := http.StatusBadRequest
	switch err {
	case room.ErrRoomNotFound:
		code = http.StatusNotFound
	case room.ErrNotHost, room.ErrNotAllowed, room.ErrSeatNotClaimable:
		code = http.StatusForbidden
	case room.ErrRoomFull, room.ErrAlreadyStarted:
		code = http.StatusConflict
	case room.ErrInvalidPhrase, room.ErrInvalidToken:
		code = http.StatusUnauthorized
	case room.ErrNotReady:
		code = http.StatusConflict
	case room.ErrRentalUnavailable:
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, gin.H{"error": err.Error()})
}

// ---- ハンドラ ----

func (s *Server) handleRoomPresets(c *gin.Context) {
	// 対応している人数を設定ファイルのrolesキーから導く。
	counts := []int{}
	for n := range s.config.Logic.Roles {
		counts = append(counts, n)
	}
	// 小さい順に返す。
	for i := 0; i < len(counts); i++ {
		for j := i + 1; j < len(counts); j++ {
			if counts[j] < counts[i] {
				counts[i], counts[j] = counts[j], counts[i]
			}
		}
	}
	presets := []map[string]any{}
	for _, n := range counts {
		roles := map[string]int{}
		for name, num := range s.config.Logic.Roles[n] {
			roles[name] = num
		}
		mode := "turn"
		if s.config.Game.Talk.Duration != nil {
			mode = "freeform"
		}
		presets = append(presets, map[string]any{
			"agent_count": n,
			"roles":       roles,
			"mode":        mode,
		})
	}
	c.JSON(http.StatusOK, gin.H{"presets": presets})
}

func (s *Server) handleCreateRoom(c *gin.Context) {
	sess, fresh := s.session(c)
	var body struct {
		RoomName    string `json:"room_name"`
		UserName    string `json:"user_name"`
		AgentCount  int    `json:"agent_count"`
		Mode        string `json:"mode"`
		IsPublic    *bool  `json:"is_public"`
		AgentSource string `json:"agent_source"`
		RentalName  string `json:"rental_name"`
		RentalSkill string `json:"rental_skill"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不正なリクエストです"})
		return
	}
	isPublic := body.IsPublic == nil || *body.IsPublic
	r, err := s.roomManager.CreateRoom(sess, room.CreateParams{
		RoomName:    body.RoomName,
		UserName:    body.UserName,
		AgentCount:  body.AgentCount,
		Mode:        body.Mode,
		Public:      isPublic,
		AgentSource: body.AgentSource,
		RentalName:  body.RentalName,
		RentalSkill: body.RentalSkill,
	})
	if err != nil {
		jsonError(c, err)
		return
	}
	if fresh {
		s.setSessionCookie(c, sess)
	}
	c.JSON(http.StatusOK, r.Project(sess))
}

func (s *Server) handleListRooms(c *gin.Context) {
	status := c.DefaultQuery("status", "active")
	if status != "active" && status != "finished" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "状態が不正です"})
		return
	}
	q := c.Query("q")
	if len([]rune(q)) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "検索語が長すぎます"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rooms": s.roomManager.ListRooms(status, q)})
}

func (s *Server) handleJoinRoom(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	s.setSessionCookie(c, sess)
	var body struct {
		Name        string `json:"name"`
		Mode        string `json:"mode"`
		AgentSource string `json:"agent_source"`
		RentalName  string `json:"rental_name"`
		RentalSkill string `json:"rental_skill"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不正なリクエストです"})
		return
	}
	if _, err := s.roomManager.Join(r, sess, body.Name, body.Mode, body.AgentSource, body.RentalName, body.RentalSkill); err != nil {
		jsonError(c, err)
		return
	}
	c.JSON(http.StatusOK, r.Project(sess))
}

func (s *Server) handleLeaveRoom(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	if err := s.roomManager.Leave(r, sess); err != nil {
		jsonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) handleCloseRoom(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	if err := s.roomManager.Close(r, sess); err != nil {
		jsonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) handleStartRoom(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	if err := s.roomManager.Start(r, sess); err != nil {
		jsonError(c, err)
		return
	}
	c.JSON(http.StatusOK, r.Project(sess))
}

func (s *Server) handleGetRoom(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	out := r.Project(sess)
	out["private_agent"] = r.PrivateView(sess)
	c.JSON(http.StatusOK, out)
}

func (s *Server) handleRoomHistory(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	cursor := 0
	if v := c.Query("cursor"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cursor = n
		}
	}
	events, seq := s.roomManager.History(r, sess, cursor)
	c.JSON(http.StatusOK, gin.H{"events": events, "cursor": seq})
}

func (s *Server) handleRoomEvents(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	ch, cancel, ok := s.roomManager.Subscribe(r, sess)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
		return
	}
	defer cancel()
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("X-Accel-Buffering", "no")
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	c.Stream(func(w io.Writer) bool {
		select {
		case payload, ok := <-ch:
			if !ok {
				return false
			}
			c.SSEvent("room", string(payload))
			return true
		case <-heartbeat.C:
			c.SSEvent("heartbeat", "{}")
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func (s *Server) handleClaimSeat(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	var body struct {
		Phrase string `json:"phrase"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不正なリクエストです"})
		return
	}
	if err := s.roomManager.Claim(r, sess, body.Phrase); err != nil {
		jsonError(c, err)
		return
	}
	c.JSON(http.StatusOK, r.Project(sess))
}

func (s *Server) handleSendAdvice(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	var body struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不正なリクエストです"})
		return
	}
	if err := s.roomManager.SendAdvice(r, sess, body.Text); err != nil {
		jsonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) handleListConsultations(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	events, _ := s.roomManager.History(r, sess, 0)
	out := []map[string]any{}
	for _, e := range events {
		if e.Type != "owner_advice" && e.Type != "owner_note" {
			continue
		}
		out = append(out, map[string]any{
			"seq": e.Seq, "type": e.Type, "text": e.Text, "day": e.Day, "at": e.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"consultations": out})
}

// handleAgentInvite は自分の席にAIを接続するためのURLと案内を返す。
// 席トークンは秘密情報なので所有者以外には返さない。
func (s *Server) handleAgentInvite(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	wsScheme := "ws"
	if c.Request.TLS != nil {
		wsScheme = "wss"
	}
	// 外部URLは明示設定を優先する。TLS終端や別ドメインを内部リスナから推測しない。
	base := s.config.Server.WebSocket.PublicURL
	if base == "" {
		u := url.URL{Host: c.Request.Host}
		base = wsScheme + "://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(s.config.Server.WebSocket.Port)) + "/ws"
	}
	invite := r.InviteFor(sess, base)
	if invite == nil {
		jsonError(c, room.ErrSeatNotClaimable)
		return
	}
	invite["kit_version"] = web.PlayerKitVersion
	invite["kit_path"] = web.PlayerKitPath
	c.Header("Cache-Control", "no-store")
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", `attachment; filename="invite.json"`)
	}
	c.JSON(http.StatusOK, invite)
}

// handleUpdateRental は待機中のレンタル席のAI名・カスタムSKILLを更新する。席の所有者のみ。
func (s *Server) handleUpdateRental(c *gin.Context) {
	r := s.roomParam(c)
	if r == nil {
		jsonError(c, room.ErrRoomNotFound)
		return
	}
	sess, _ := s.session(c)
	var body struct {
		RentalName  string `json:"rental_name"`
		RentalSkill string `json:"rental_skill"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不正なリクエストです"})
		return
	}
	if err := s.roomManager.UpdateRental(r, sess, body.RentalName, body.RentalSkill); err != nil {
		jsonError(c, err)
		return
	}
	c.JSON(http.StatusOK, r.Project(sess))
}

// handleRentalCapabilities はレンタルAIの利用可否を返す。キー・内部設定は返さない。
func (s *Server) handleRentalCapabilities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, s.roomManager.RentalCapabilities())
}
