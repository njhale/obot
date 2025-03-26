package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gptscript-ai/go-gptscript"
	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/logger"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/api/authn"
	"github.com/obot-platform/obot/pkg/api/authz"
	gclient "github.com/obot-platform/obot/pkg/gateway/client"
	"github.com/obot-platform/obot/pkg/proxy"
	"github.com/obot-platform/obot/pkg/storage"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var log = logger.Package()

type Server struct {
	storageClient storage.Client
	gatewayClient *gclient.Client
	gptClient     *gptscript.GPTScript
	authenticator *authn.Authenticator
	authorizer    *authz.Authorizer
	proxyManager  *proxy.Manager
	baseURL       string

	mux *http.ServeMux
}

func NewServer(storageClient storage.Client, gatewayClient *gclient.Client, gptClient *gptscript.GPTScript, authn *authn.Authenticator, authz *authz.Authorizer, proxyManager *proxy.Manager, baseURL string) *Server {
	return &Server{
		storageClient: storageClient,
		gatewayClient: gatewayClient,
		gptClient:     gptClient,
		authenticator: authn,
		authorizer:    authz,
		proxyManager:  proxyManager,
		baseURL:       baseURL + "/api",

		mux: http.NewServeMux(),
	}
}

func (s *Server) HandleFunc(pattern string, f api.HandlerFunc) {
	s.mux.HandleFunc(pattern, s.wrap(f))
}

func (s *Server) HTTPHandle(pattern string, f http.Handler) {
	s.HandleFunc(pattern, func(req api.Context) error {
		f.ServeHTTP(req.ResponseWriter, req.Request)
		return nil
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) wrap(f api.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		user, err := s.authenticator.Authenticate(req)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusUnauthorized)
			return
		}

		if strings.HasPrefix(req.URL.Path, "/api/") && user.GetUID() != "" && user.GetUID() != "anonymous" {
			// Best effort
			if err := s.gatewayClient.AddActivityForToday(req.Context(), user.GetUID()); err != nil {
				log.Warnf("Failed to add activity tracking for user %s: %v", user.GetName(), err)
			}
		}

		if user.GetExtra()["set-cookies"] != nil {
			for _, setCookie := range user.GetExtra()["set-cookies"] {
				rw.Header().Add("Set-Cookie", setCookie)
			}
		}

		if !s.authorizer.Authorize(req, user) {
			if _, err := req.Cookie("obot_access_token"); err == nil && req.URL.Path == "/api/me" {
				// Tell the browser to delete the obot_access_token cookie.
				// If the user tried to access this path and was unauthorized, then something is wrong with their token.
				http.SetCookie(rw, &http.Cookie{
					Name:   "obot_access_token",
					Value:  "",
					Path:   "/",
					MaxAge: -1,
				})
			}

			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}

		if strings.HasPrefix(req.URL.Path, "/api/") {
			rw.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate, max-age=0")
			rw.Header().Set("Pragma", "no-cache")
			rw.Header().Set("Expires", "0")
		}

		err = f(api.Context{
			ResponseWriter: rw,
			Request:        req,
			GPTClient:      s.gptClient,
			Storage:        s.storageClient,
			GatewayClient:  s.gatewayClient,
			User:           user,
			APIBaseURL:     s.baseURL,
		})
		if errHTTP := (*types.ErrHTTP)(nil); errors.As(err, &errHTTP) {
			http.Error(rw, errHTTP.Message, errHTTP.Code)
		} else if errStatus := (*apierrors.StatusError)(nil); errors.As(err, &errStatus) {
			http.Error(rw, errStatus.Error(), int(errStatus.ErrStatus.Code))
		} else if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
	}
}
