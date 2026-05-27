package routes

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/api/handler"
	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/db"
	"github.com/rizxfrog/oh-my-api/internal/middleware"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

func New(deps model.Dependencies, store *db.Store, bootstrap *handler.BootstrapManager, codebuddyClient *proxy.CodeBuddyClient) http.Handler {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	s := &handler.Server{
		Deps:               deps,
		DB:                 store,
		StoreExecutionLogs: deps.StoreExecutionLogs,
		Bootstrap:          bootstrap,
		TokenStats:         deps.TokenStats,
		RequestStats:       deps.RequestStats,
		CodeBuddyClient:    codebuddyClient,
	}

	mux := http.NewServeMux()
	registerLingmaRoutes(mux, s)
	registerCodeBuddyRoutes(mux, s)
	registerAdminRoutes(mux, s)

	if deps.FrontendFS != (embed.FS{}) {
		subFS, err := fs.Sub(deps.FrontendFS, "frontend-dist")
		if err == nil {
			fileServer := http.FileServerFS(subFS)
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				f, err := subFS.Open(strings.TrimPrefix(r.URL.Path, "/"))
				if err == nil {
					f.Close()
					fileServer.ServeHTTP(w, r)
					return
				}
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
			})
		}
	}

	hdlr := http.Handler(mux)
	if store != nil {
		settings, _ := store.GetSettings(context.Background())
		cfg := middleware.LoggingConfig{
			StorageMode:    settings["storage_mode"],
			TruncateLength: parseIntOr(settings["truncate_length"], 102400),
		}
		hdlr = middleware.Logging(store, cfg)(hdlr)
	}
	return hdlr
}

func parseIntOr(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}
