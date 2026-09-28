package transport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/aiwolfdial/aiwolf-nlp-server/matchmaking"
	"github.com/aiwolfdial/aiwolf-nlp-server/model"
	"github.com/aiwolfdial/aiwolf-nlp-server/observer"
	"github.com/aiwolfdial/aiwolf-nlp-server/observer/livestate"
	"github.com/aiwolfdial/aiwolf-nlp-server/orchestrator"
	"github.com/aiwolfdial/aiwolf-nlp-server/room"
	"github.com/aiwolfdial/aiwolf-nlp-server/service"
	"github.com/aiwolfdial/aiwolf-nlp-server/util"
	"github.com/gorilla/websocket"
)

type Server struct {
	config              model.Config
	upgrader            websocket.Upgrader
	manager             *orchestrator.GameManager
	roomManager         *room.Manager
	liveState           *livestate.LiveState
	jsonLogger          *service.JSONLogger
	gameLogger          *service.GameLogger
	realtimeBroadcaster *service.RealtimeBroadcaster
	ttsBroadcaster      *service.TTSBroadcaster
}

func NewServer(config model.Config) (*Server, error) {
	if raw := config.Server.WebSocket.PublicURL; raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("公開WebSocket URLには認証情報・クエリなしのws://またはwss:// URLを指定してください")
		}
	}
	server := &Server{
		config: config,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		liveState: livestate.New(),
	}
	gameSettings, err := model.NewSetting(config)
	if err != nil {
		return nil, errors.New("ゲーム設定の作成に失敗しました")
	}
	if config.JSONLogger.Enable {
		server.jsonLogger = service.NewJSONLogger(config)
	}
	if config.GameLogger.Enable {
		server.gameLogger = service.NewGameLogger(config)
	}
	if config.TTSBroadcaster.Enable {
		server.ttsBroadcaster = service.NewTTSBroadcaster(config)
	}
	if config.RealtimeBroadcaster.Enable {
		server.realtimeBroadcaster = service.NewRealtimeBroadcaster(config)
	}
	var matchOptimizer *matchmaking.MatchOptimizer
	if config.Matching.IsOptimize {
		matchOptimizer, err = matchmaking.NewMatchOptimizer(config)
		if err != nil {
			return nil, errors.New("マッチオプティマイザの作成に失敗しました")
		}
	}
	server.manager = orchestrator.NewGameManager(config, gameSettings, matchmaking.NewWaitingRoom(config), matchOptimizer, server.newObserver)
	if config.Server.Web.Enable {
		server.roomManager = room.NewManager(config, server.newObserver)
	}
	return server, nil
}

func (s *Server) newObserver() observer.GameObserver {
	var observers []observer.GameObserver
	if s.jsonLogger != nil {
		observers = append(observers, s.jsonLogger.AsObserver())
	}
	if s.gameLogger != nil {
		observers = append(observers, s.gameLogger.AsObserver())
	}
	if s.realtimeBroadcaster != nil {
		observers = append(observers, s.realtimeBroadcaster.AsObserver())
	}
	if s.ttsBroadcaster != nil {
		observers = append(observers, s.ttsBroadcaster.AsObserver())
	}
	if s.liveState != nil {
		observers = append(observers, s.liveState)
	}
	return observer.NewComposite(observers...)
}

func (s *Server) Run() {
	router := s.buildRouter()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		slog.Info("シグナルを受信しました")
		s.manager.BeginShutdown()
		s.manager.WaitAllFinished()
		stop()
		os.Exit(0)
	}()

	errCh := make(chan error, 2)
	wsAddr := s.config.Server.WebSocket.Host + ":" + strconv.Itoa(s.config.Server.WebSocket.Port)
	slog.Info("AI用WebSocketサーバを起動しました", "addr", wsAddr)
	go func() {
		errCh <- router.Run(wsAddr)
	}()

	if s.config.Server.Web.Enable && s.roomManager != nil {
		webRouter := s.buildWebRouter()
		webAddr := s.config.Server.Web.Host + ":" + strconv.Itoa(s.config.Server.Web.Port)
		slog.Info("Webサーバを起動しました", "addr", webAddr)
		go func() {
			errCh <- webRouter.Run(webAddr)
		}()
	}

	if err := <-errCh; err != nil {
		slog.Error("サーバの起動に失敗しました", "error", err)
	}
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	if s.manager.IsShuttingDown() {
		slog.Warn("シグナルを受信したため、新しい接続を受け付けません")
		return
	}
	header := r.Header.Clone()
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("クライアントのアップグレードに失敗しました", "error", err)
		return
	}
	conn, err := model.NewConnection(ws, &header)
	if err != nil {
		slog.Error("クライアントの接続に失敗しました", "error", err)
		return
	}
	if s.config.Server.Authentication.Enable {
		token := r.URL.Query().Get("token")
		if token != "" {
			if !util.IsValidPlayerToken(os.Getenv("SECRET_KEY"), token, conn.TeamName) {
				slog.Warn("トークンが無効です", "team_name", conn.TeamName)
				conn.Conn.Close()
				slog.Info("クライアントの接続を切断しました", "team_name", conn.TeamName)
				return
			}
		} else {
			token = strings.ReplaceAll(conn.Header.Get("Authorization"), "Bearer ", "")
			if !util.IsValidPlayerToken(os.Getenv("SECRET_KEY"), token, conn.TeamName) {
				slog.Warn("トークンが無効です", "team_name", conn.TeamName)
				conn.Conn.Close()
				slog.Info("クライアントの接続を切断しました", "team_name", conn.TeamName)
				return
			}
		}
	}

	// seat_token付き接続はルームの席に割り当てる。無ければ従来の待機部屋へ。
	if s.roomManager != nil {
		if roomID := r.URL.Query().Get("room_id"); roomID != "" {
			token := r.URL.Query().Get("seat_token")
			if err := s.roomManager.AgentJoin(roomID, token, conn); err != nil {
				slog.Warn("席への参加を拒否しました", "error", err)
				conn.Conn.Close()
				return
			}
			// 接続は席に保持され、ゲーム開始まで待機する。
			return
		}
	}

	s.manager.TryStartGame(*conn)
}
