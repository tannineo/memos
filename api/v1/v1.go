package v1

import (
	"github.com/labstack/echo/v4"
	"github.com/usememos/memos/server/profile"
	"github.com/usememos/memos/store"
)

type APIV1Service struct {
	Secret  string
	Profile *profile.Profile
	Store   *store.Store
}

func NewAPIV1Service(secret string, profile *profile.Profile, store *store.Store) *APIV1Service {
	return &APIV1Service{
		Secret:  secret,
		Profile: profile,
		Store:   store,
	}
}

func (s *APIV1Service) Register(rootGroup *echo.Group) {
	// Register RSS routes.
	s.registerRSSRoutes(rootGroup)

	// Register API v1 routes.
	apiV1Group := rootGroup.Group("/api/v1")
	apiV1Group.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return JWTMiddleware(s, next, s.Secret)
	})
	s.registerSystemRoutes(apiV1Group)
	s.registerSystemSettingRoutes(apiV1Group)
	s.registerAuthRoutes(apiV1Group)
	s.registerIdentityProviderRoutes(apiV1Group)
	s.registerUserRoutes(apiV1Group)
	s.registerUserSettingRoutes(apiV1Group)
	s.registerTagRoutes(apiV1Group)
	s.registerStorageRoutes(apiV1Group)
	s.registerResourceRoutes(apiV1Group)
	s.registerMemoRoutes(apiV1Group)
	s.registerMemoOrganizerRoutes(apiV1Group)
	s.registerMemoResourceRoutes(apiV1Group)
	s.registerMemoRelationRoutes(apiV1Group)

	// Register public routes.
	publicGroup := rootGroup.Group("/o")
	publicGroup.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return JWTMiddleware(s, next, s.Secret)
	})
	s.registerGetterPublicRoutes(publicGroup)
	s.registerResourcePublicRoutes(publicGroup)
}
