package handler

import (
	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/db"
	"github.com/rizxfrog/oh-my-api/internal/redis"
)

// Server is the central HTTP handler. All route handlers are methods on Server.
// Create a Server via NewServer from the router package.
type Server struct {
	Deps               model.Dependencies
	DB                 *db.Store
	StoreExecutionLogs bool
	Bootstrap          *BootstrapManager
	TokenStats         *redis.TokenStats
	RequestStats       *redis.RequestStats
}
