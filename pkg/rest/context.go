package rest

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo"
	"github.com/luxas/digitized/pkg/apis/digitized.luxaslabs.com/v1alpha1"
	"github.com/luxas/digitized/pkg/apis/meta"
	"github.com/weaveworks/libgitops/pkg/runtime"
	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage"
	kruntime "k8s.io/apimachinery/pkg/runtime"
)

type StorageContext interface {
	echo.Context
	// Warn headers
	Warn(err error)
	Warnf(format string, args ...interface{})
	// Access to the Storage
	Storage() storage.Storage
}

type ResourceContext interface {
	StorageContext

	Resource() GroupVersionKindResource
	// Conditionally defaults the answer when returning
	JSONIndent(code int, obj runtime.Object) error
	KindKey() storage.KindKey
}

type NamespacedResourceContext interface {
	ResourceContext
	Namespace() string
}

type NamedResourceContext interface {
	ResourceContext
	Name() string
	ObjectKey() storage.ObjectKey
}

var _ StorageContext = &storageContextImpl{}

type storageContextImpl struct {
	echo.Context
	s storage.Storage
}

func (cc *storageContextImpl) Warn(err error) {
	cc.Warnf(err.Error())
}

func (cc *storageContextImpl) Warnf(format string, args ...interface{}) {
	cc.Response().Header().Add("Warning", fmt.Sprintf(format, args...))
}

func (cc *storageContextImpl) Storage() storage.Storage {
	return cc.s
}

func storageContextMiddleware(s storage.Storage) func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := &storageContextImpl{c, s}
			return next(cc)
		}
	}
}

var _ ResourceContext = &resourceContextImpl{}

type resourceContextImpl struct {
	StorageContext
	gvkr GroupVersionKindResource
}

func (cc *resourceContextImpl) Resource() GroupVersionKindResource {
	return cc.gvkr
}

func (cc *resourceContextImpl) JSONIndent(code int, obj runtime.Object) error {
	// Do automatic conditional defaulting of the returned response
	// TODO: Do defaulting before validation?
	if cc.ShouldDefault() {
		objs := []kruntime.Object{obj}            // default case
		if list, ok := obj.(*v1alpha1.List); ok { // list case
			objs = list.Items
		}
		if err := cc.Storage().Serializer().Defaulter().Default(objs...); err != nil {
			return err
		}
	}

	cc.Response().WriteHeader(code)
	return cc.Storage().Serializer().
		Encoder(serializer.WithPrettyEncode(true)).
		EncodeForGroupVersion(serializer.NewJSONFrameWriter(cc.Response()), obj, cc.gvkr.GVK().GroupVersion())
}

func (cc *resourceContextImpl) KindKey() storage.KindKey {
	return storage.NewKindKey(cc.gvkr.GVK())
}

func (cc *resourceContextImpl) ShouldDefault() bool {
	h := cc.Request().Header.Get("default")
	return len(h) == 0 || h == "true" // default to true for the empty header case
}

func resourceContextMiddleware(gvkr GroupVersionKindResource) func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := &resourceContextImpl{c.(StorageContext), gvkr}
			return next(cc)
		}
	}
}

var _ NamespacedResourceContext = &namespacedResourceContextImpl{}

type namespacedResourceContextImpl struct {
	ResourceContext
	namespace string
}

func (cc *namespacedResourceContextImpl) Namespace() string {
	return cc.namespace
}

func namespacedResourceContextMiddleware() func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rc := c.(ResourceContext)
			namespace := rc.Param("namespace")
			if !rc.Resource().Namespaced {
				return newStatusErrorf(http.StatusInternalServerError, "NamespacedResourceContext middleware requires a Namespaced resource")
			}
			if namespace == "" {
				return newStatusErrorf(http.StatusBadRequest, "namespace parameter is mandatory")
			}
			cc := &namespacedResourceContextImpl{rc, namespace}
			return next(cc)
		}
	}
}

var _ NamedResourceContext = &namedResourceContextImpl{}

type namedResourceContextImpl struct {
	ResourceContext
	name string
}

func (cc *namedResourceContextImpl) Name() string {
	return cc.name
}

func (cc *namedResourceContextImpl) ObjectKey() storage.ObjectKey {
	if ns, ok := cc.ResourceContext.(NamespacedResourceContext); ok {
		return storage.NewObjectKey(
			cc.KindKey(),
			meta.NameNamespaceIdentifier(ns.Namespace(), cc.name),
		)
	}
	return storage.NewObjectKey(cc.KindKey(), meta.NameIdentifier(cc.name))
}

func namedResourceContextMiddleware() func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rc := c.(ResourceContext)
			name := rc.Param("name")
			if name == "" {
				return newStatusErrorf(http.StatusBadRequest, "name parameter is mandatory")
			}
			cc := &namedResourceContextImpl{rc, name}
			return next(cc)
		}
	}
}
