package rest

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/weaveworks/libgitops/pkg/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
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
	e.Use(middleware.CORS())
	e.HTTPErrorHandler = errorHandler(e)
	e.GET("/", routes)
	r := &RESTServer{e, addr, s, nil}
	r.agh = NewAPIGroupsHandler(r, ignoredGroups, ignoredKinds)
	return r, nil
}

func newStatusError(code int32, err error) *statusErrorImpl {
	return &statusErrorImpl{InternalStatus: metav1.Status{Code: code, Message: err.Error()}}
}

func newStatusErrorf(code int32, format string, args ...interface{}) *statusErrorImpl {
	return &statusErrorImpl{InternalStatus: metav1.Status{Code: code, Message: fmt.Sprintf(format, args...)}}
}

type statusErrorImpl struct {
	InternalStatus metav1.Status
}

func (se statusErrorImpl) Error() string {
	return se.InternalStatus.String()
}

// Status implements statusError in k8s.io/apiserver/pkg/endpoints/handlers/responsewriters
func (se statusErrorImpl) Status() metav1.Status {
	return se.InternalStatus
}

func errorHandler(e *echo.Echo) func(httpErr error, c echo.Context) {
	return func(httpErr error, c echo.Context) {
		switch err := httpErr.(type) {
		case *echo.HTTPError:
			httpErr = newStatusErrorf(int32(err.Code), fmt.Sprintf("%v", err.Message))
		}
		status := responsewriters.ErrorToAPIStatus(httpErr)

		if err := c.JSONPretty(int(status.Code), status, "  "); err != nil {
			e.Logger.Errorf("errorHandler: Error when sending JSON response: %v", err)
		}
	}
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
