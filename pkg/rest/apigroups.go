package rest

import (
	"net/http"
	"strings"

	"github.com/jinzhu/inflection"
	"github.com/labstack/echo"
	"github.com/luxas/digitized/pkg/apis/meta"
	"github.com/weaveworks/libgitops/pkg/storage"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type GroupVersionKindResource struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Resource   string `json:"resource"`
	Namespaced bool   `json:"namespaced"`
	Singleton  bool   `json:"singleton"`
}

func (gvkr GroupVersionKindResource) GVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: gvkr.Group, Version: gvkr.Version, Kind: gvkr.Kind}
}

type RESTStorage interface {
	Echo() *echo.Echo
	Storage() storage.Storage
	APIGroups() APIGroupsHandler
	ListenBlocking() error
}

type APIGroupsHandler interface {
	GroupVersion(gv schema.GroupVersion) (GroupVersionHandler, bool)
}

type GroupVersionHandler interface {
	Kind(kind string) (ResourceHandler, bool)
}

type ResourceHandler interface {
	Resource() GroupVersionKindResource
	RegisterSubResource(name string, fn ResourceHookHandler)
	RegisterNamedSubResource(name string, fn NamedResourceHookHandler)
	RegisterPOSTHook(handler ObjectHookHandler)
	RegisterDELETEHook(handler NamedResourceHookHandler)
}

func NewAPIGroupsHandler(rest RESTStorage,
	ignoredGroups map[schema.GroupVersion]struct{},
	ignoredKinds map[schema.GroupVersionKind]struct{}) APIGroupsHandler {
	groupsHandler := rest.Echo().Group("/apis")
	agh := &apiGroupsHandlerImpl{groupsHandler, make(map[schema.GroupVersion]GroupVersionHandler), rest.Storage().Serializer().Scheme()}
	groupsHandler.GET("/", agh.info)
	for _, gv := range agh.scheme.PrioritizedVersionsAllGroups() {
		if _, ok := ignoredGroups[gv]; ok {
			continue
		}
		agh.apiGroups[gv] = newGroupVersionHandler(agh.g, gv, agh.scheme, ignoredKinds)
	}
	return agh
}

type apiGroupsHandlerImpl struct {
	g         *echo.Group
	apiGroups map[schema.GroupVersion]GroupVersionHandler
	scheme    *runtime.Scheme
}

func (agh *apiGroupsHandlerImpl) info(c echo.Context) error {
	apiGroupNames := make([]string, 0, len(agh.apiGroups))
	for gv := range agh.apiGroups {
		apiGroupNames = append(apiGroupNames, gv.String())
	}
	return c.JSON(http.StatusOK, apiGroupNames)
}

func (agh *apiGroupsHandlerImpl) GroupVersion(gv schema.GroupVersion) (gh GroupVersionHandler, ok bool) {
	gh, ok = agh.apiGroups[gv]
	return
}

func newGroupVersionHandler(parent *echo.Group, gv schema.GroupVersion, scheme *runtime.Scheme, ignoredKinds map[schema.GroupVersionKind]struct{}) GroupVersionHandler {
	groupHandler := parent.Group("/" + gv.String())
	gvh := &groupVersionHandlerImpl{gv, groupHandler, make(map[schema.GroupVersionKind]ResourceHandler), scheme}
	groupHandler.GET("/", gvh.info)

	// TODO: RawStorage should automatically mkdir

	for kind := range scheme.KnownTypes(gv) {
		gvk := gv.WithKind(kind)

		// Don't show ignored kinds
		if _, ok := ignoredKinds[gvk]; ok {
			continue
		}

		obj, err := scheme.New(gvk)
		if err != nil {
			panic(err)
		}
		isSingleton := meta.IsSingleton(obj)
		// Pluralize if it is not a singleton
		if !isSingleton {
			kind = inflection.Plural(kind)
		}
		resourceName := strings.ToLower(kind)
		gvkr := GroupVersionKindResource{gvk.Group, gvk.Version, gvk.Kind, resourceName, meta.IsNamespaced(obj), isSingleton}
		gvh.resources[gvk] = newResourceHandler(gvh.g, gvkr)
	}
	return gvh
}

type groupVersionHandlerImpl struct {
	gv        schema.GroupVersion
	g         *echo.Group
	resources map[schema.GroupVersionKind]ResourceHandler
	scheme    *runtime.Scheme
}

type apiGroupInfo struct {
	// TODO: De-dup these, while still having sensible JSON output
	Group   string `json:"group"`
	Version string `json:"version"`

	Resources []GroupVersionKindResource `json:"resources"`
}

func (gvh *groupVersionHandlerImpl) info(c echo.Context) error {
	agi := &apiGroupInfo{
		Group:   gvh.gv.Group,
		Version: gvh.gv.Version,
	}
	for _, rh := range gvh.resources {
		agi.Resources = append(agi.Resources, rh.Resource())
	}
	return c.JSONPretty(http.StatusOK, agi, "  ")
}

func (gvh *groupVersionHandlerImpl) Kind(kind string) (rh ResourceHandler, ok bool) {
	rh, ok = gvh.resources[gvh.gv.WithKind(kind)]
	return
}
