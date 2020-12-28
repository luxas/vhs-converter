package rest

import (
	"net/http"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/weaveworks/libgitops/pkg/storage"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TODO: Change to opt-in instead of opt-out?
func NewRESTServer(addr string, s storage.Storage,
	ignoredGroups map[schema.GroupVersion]struct{},
	ignoredKinds map[schema.GroupVersionKind]struct{}) (RESTStorage, error) {
	e := echo.New()
	e.Use(storageContextMiddleware(s))
	e.Pre(middleware.AddTrailingSlash())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.GET("/", routes)
	r := &RESTServer{e, addr, s, nil}
	r.agh = NewAPIGroupsHandler(r, ignoredGroups, ignoredKinds)
	return r, nil
}

type RESTServer struct {
	e    *echo.Echo
	addr string
	s    storage.Storage
	agh  APIGroupsHandler
}

func (s *RESTServer) ListenBlocking() error {
	return s.e.Start(s.addr)
}

func (s *RESTServer) Echo() *echo.Echo {
	return s.e
}

func (s *RESTServer) Storage() storage.Storage {
	return s.s
}

func (s *RESTServer) APIGroups() APIGroupsHandler {
	return s.agh
}

func routes(c echo.Context) error {
	return c.JSONPretty(http.StatusOK, c.Echo().Routes(), "  ")
}
