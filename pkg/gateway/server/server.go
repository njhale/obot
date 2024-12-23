package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/obot-platform/obot/pkg/gateway/client"
	"github.com/obot-platform/obot/pkg/gateway/db"
	"github.com/obot-platform/obot/pkg/gateway/server/dispatcher"
	"github.com/obot-platform/obot/pkg/gateway/types"
	"github.com/obot-platform/obot/pkg/jwt"
	"gorm.io/gorm"
)

type Options struct {
	Hostname     string
	UIHostname   string `name:"ui-hostname" env:"OBOT_SERVER_UI_HOSTNAME"`
	GatewayDebug bool
}

type Server struct {
	adminEmails     map[string]struct{}
	db              *db.DB
	baseURL, uiURL  string
	httpClient      *http.Client
	client          *client.Client
	tokenService    *jwt.TokenService
	modelDispatcher *dispatcher.Dispatcher
}

func New(ctx context.Context, db *db.DB, tokenService *jwt.TokenService, modelProviderDispatcher *dispatcher.Dispatcher, adminEmails []string, opts Options) (*Server, error) {
	if err := db.AutoMigrate(); err != nil {
		return nil, fmt.Errorf("auto migrate failed: %w", err)
	}

	adminEmailsSet := make(map[string]struct{}, len(adminEmails))
	for _, email := range adminEmails {
		adminEmailsSet[email] = struct{}{}
	}

	s := &Server{
		adminEmails:     adminEmailsSet,
		db:              db,
		baseURL:         opts.Hostname,
		uiURL:           opts.UIHostname,
		httpClient:      &http.Client{},
		client:          client.New(db, adminEmails),
		tokenService:    tokenService,
		modelDispatcher: modelProviderDispatcher,
	}

	go s.autoCleanupTokens(ctx)
	go s.oAuthCleanup(ctx)

	return s, nil
}

func (s *Server) UpsertAuthProvider(ctx context.Context, configType, clientID, clientSecret string) (uint, error) {
	if clientID == "" || clientSecret == "" {
		return 0, nil
	}

	authProvider := &types.AuthProvider{
		Type:          configType,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		OAuthURL:      types.OAuthURLByType(configType),
		JWKSURL:       types.JWKSURLByType(configType),
		TokenURL:      types.TokenURLByType(configType),
		ServiceName:   strings.ToTitle(string(configType[0])) + configType[1:],
		Scopes:        types.ScopesByType(configType),
		UsernameClaim: types.UsernameClaimByType(configType),
		EmailClaim:    types.EmailClaimByType(configType),
		Slug:          strings.ToLower(configType),
		Expiration:    "7d",
		ExpirationDur: 7 * 24 * time.Hour,
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing := new(types.AuthProvider)
		if err := tx.WithContext(ctx).Where("slug = ?", authProvider.Slug).First(existing).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if existing.ID == 0 {
			return tx.WithContext(ctx).Create(authProvider).Error
		}

		authProvider.Model = existing.Model
		return tx.WithContext(ctx).Model(authProvider).Updates(authProvider).Error
	}); err != nil {
		return 0, err
	}

	return authProvider.ID, nil
}
