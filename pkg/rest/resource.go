package rest

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/labstack/echo"
	"github.com/luxas/digitized/pkg/apis/digitized.luxaslabs.com/v1alpha1"
	"github.com/luxas/digitized/pkg/apis/meta"
	"github.com/weaveworks/libgitops/pkg/runtime"
	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage"
	kruntime "k8s.io/apimachinery/pkg/runtime"
)

type (
	// ResourceHookHandler is a hook handler that operates in a resource context.
	ResourceHookHandler func(ResourceContext) error
	// NamedResourceHookHandler is a hook handler that operates in a named resource context.
	NamedResourceHookHandler func(NamedResourceContext) error
	// ObjectHookHandler is a hook handler that operates in resource context, where there
	// is JSON data in the body that has been is decoded. The hook executes before data is
	// saved to the storage though, so obj is a pointer that can be mutated.
	ObjectHookHandler func(rc ResourceContext, obj runtime.Object) error
)

func newResourceHandler(parent *echo.Group, gvkr GroupVersionKindResource) ResourceHandler {
	g := parent.Group("/"+gvkr.Resource, resourceContextMiddleware(gvkr))
	rh := &resourceHandlerImpl{g, gvkr, nil, nil, nil}
	// Register the relevant routes
	g.POST("/", rh.create)
	g.PATCH("/", rh.patch)
	g.PUT("/", rh.update)
	// Singletons don't have named routes, nor LIST
	if gvkr.Singleton {
		g.GET("/", rh.getSingleton)
	} else {
		g.GET("/", rh.list)
		g.GET(rh.namedRoute(), rh.get, namedResourceContextMiddleware())
		g.DELETE(rh.namedRoute(), rh.delete, namedResourceContextMiddleware())
	}
	return rh
}

type resourceHandlerImpl struct {
	g           *echo.Group
	gvkr        GroupVersionKindResource
	postHooks   []ObjectHookHandler
	putHooks    []ObjectHookHandler
	deleteHooks []NamedResourceHookHandler
}

func (rh *resourceHandlerImpl) Resource() GroupVersionKindResource {
	return rh.gvkr
}

func (rh *resourceHandlerImpl) RegisterSubResource(name string, fn ResourceHookHandler) {
	rh.g.PUT("/"+name+"/", func(c echo.Context) error {
		return fn(c.(ResourceContext))
	})
}

func (rh *resourceHandlerImpl) RegisterNamedSubResource(name string, fn NamedResourceHookHandler) {
	rh.g.PUT("/"+rh.namedRoute()+"/"+name+"/", func(c echo.Context) error {
		return fn(c.(NamedResourceContext))
	}, namedResourceContextMiddleware())
}

func (rh *resourceHandlerImpl) RegisterPOSTHook(handler ObjectHookHandler) {
	rh.postHooks = append(rh.postHooks, handler)
}

func (rh *resourceHandlerImpl) RegisterPUTHook(handler ObjectHookHandler) {
	rh.putHooks = append(rh.putHooks, handler)
}

func (rh *resourceHandlerImpl) RegisterDELETEHook(handler NamedResourceHookHandler) {
	rh.deleteHooks = append(rh.deleteHooks, handler)
}

func (rh *resourceHandlerImpl) validateBody(rc ResourceContext, obj runtime.Object) error {
	// Only validate ObjectMeta for non-singletons
	if !rh.gvkr.Singleton {
		if obj.GetName() == "" {
			return fmt.Errorf("name must be set")
		}
		// Validate namespace
		// TODO: Make namespace a tri-state (never namespace, default: use default ns, always set)?
		if meta.IsNamespaced(obj) {
			if obj.GetNamespace() == "" {
				return fmt.Errorf(".metadata.namespace must be set for namespaced object")
			}
		} else {
			if obj.GetNamespace() != "" {
				rc.Warnf("Non-namespaced resources must not have the .metadata.namespace field set. Pruning the field")
				obj.SetNamespace("")
			}
		}
	}
	// Validate the object if possible
	return meta.ValidateIfPossible(obj)
}

func (rh *resourceHandlerImpl) namedRoute() string {
	if rh.gvkr.Namespaced {
		return "/:namespace/:name/"
	}
	return "/:name/"
}

func (rh *resourceHandlerImpl) list(c echo.Context) error {
	rc := c.(ResourceContext)
	list, err := rc.Storage().List(rc.KindKey())
	if err != nil {
		return rc.Errorf(http.StatusInternalServerError, err)
	}
	// Need to do dummy conversion
	runtimeList := make([]kruntime.Object, 0, len(list))
	for _, item := range list {
		runtimeList = append(runtimeList, item)
	}

	return rc.JSONIndent(http.StatusOK, &v1alpha1.List{Items: runtimeList})
}

func (rh *resourceHandlerImpl) getSingleton(c echo.Context) error {
	rc := c.(ResourceContext)

	obj, err := rc.Storage().Get(meta.SingletonKey(rc.KindKey()))
	if err != nil {
		return err
	}
	return rc.JSONIndent(http.StatusOK, obj)
}

func (rh *resourceHandlerImpl) decodeObject(rc ResourceContext) (runtime.Object, error) {
	// Decode the body and cast it to libgitops runtime.Object
	kobj, err := rc.Storage().Serializer().Decoder().Decode(serializer.NewJSONFrameReader(rc.Request().Body))
	if err != nil {
		return nil, rc.Errorf(http.StatusBadRequest, err)
	}
	obj, ok := kobj.(runtime.Object)
	if !ok {
		return nil, rc.Errorf(http.StatusInternalServerError, errors.New("couldn't cast decoded object to runtime.Object"))
	}

	// Validate the body
	if err := rh.validateBody(rc, obj); err != nil {
		return nil, rc.Errorf(http.StatusBadRequest, err)
	}
	return obj, nil
}

func (rh *resourceHandlerImpl) create(c echo.Context) error {
	rc := c.(ResourceContext)

	obj, err := rh.decodeObject(rc)
	if err != nil {
		return err
	}

	// Run all hooks in order
	for _, hook := range rh.postHooks {
		if err := hook(rc, obj); err != nil {
			return err
		}
	}

	// Write to storage
	if err := rc.Storage().Create(obj); err != nil {
		return rc.Errorf(http.StatusInternalServerError, err)
	}

	return rc.JSONIndent(http.StatusCreated, obj)
}

func (rh *resourceHandlerImpl) patch(c echo.Context) error {
	rc := c.(ResourceContext)

	// We need to read the patch twice, once for getting the ObjectKey info
	patch, err := ioutil.ReadAll(rc.Request().Body)
	if err != nil {
		return err
	}
	// rc.Request().Body.Close() // do we need this?

	// Decode the patch to a so-called "Partial Object" to extract the ObjectKey
	objs, err := storage.DecodePartialObjects(
		ioutil.NopCloser(bytes.NewReader(patch)),
		rc.Storage().Serializer().Scheme(),
		false, // guarantees that len(objs)==1 when err==nil
		nil,   // no default GVK although we "could", the caller should be specific
	)
	if err != nil {
		return rc.Errorf(http.StatusBadRequest, fmt.Errorf("couldn't decode patch: %w", err))
	}
	// Wrap the decoded object in an extension that knows how to propagate namespaced and singleton
	// information. Now, extract the correct ObjectKey for this patch
	patchObj := &customPartialObject{objs[0], rh.gvkr.Namespaced, rh.gvkr.Singleton}
	key, err := rc.Storage().ObjectKeyFor(patchObj)
	if err != nil {
		return err
	}

	// TODO: Verify that GVKR & decoded object GVK match
	// TODO: Fix Storage.Patch to work with RawStorage & YAML

	// Write the patch to the underlying storage
	// TODO: Make sure we validate the new payload here before applying
	if err := rc.Storage().Patch(key, patch); err != nil {
		return err
	}
	// After doing the patch, return the new object
	obj, err := rc.Storage().Get(key)
	if err != nil {
		return err
	}
	return rc.JSONIndent(http.StatusOK, obj)
}

func (rh *resourceHandlerImpl) update(c echo.Context) error {
	rc := c.(ResourceContext)

	obj, err := rh.decodeObject(rc)
	if err != nil {
		return err
	}

	// Run all hooks in order
	for _, hook := range rh.putHooks {
		if err := hook(rc, obj); err != nil {
			return err
		}
	}

	// Write to storage
	if err := rc.Storage().Update(obj); err != nil {
		return rc.Errorf(http.StatusInternalServerError, err)
	}

	return rc.JSONIndent(http.StatusOK, obj)
}

func (rh *resourceHandlerImpl) get(c echo.Context) error {
	rc := c.(NamedResourceContext)

	obj, err := rc.Storage().Get(rc.ObjectKey())
	if err != nil {
		return err
	}
	return rc.JSONIndent(http.StatusOK, obj)
}

func (rh *resourceHandlerImpl) delete(c echo.Context) error {
	rc := c.(NamedResourceContext)

	// Run all hooks in order
	for _, hook := range rh.deleteHooks {
		if err := hook(rc); err != nil {
			return err
		}
	}

	if err := rc.Storage().Delete(rc.ObjectKey()); err != nil {
		return rc.Errorf(http.StatusInternalServerError, err)
	}
	return rc.NoContent(http.StatusNoContent)
}

// customPartialObject is a superset of runtime.PartialObject that implements the
// Namespaced and Singleton interfaces in the way the Storage's identifier expects.
type customPartialObject struct {
	runtime.PartialObject
	namespaced bool
	singleton  bool
}

func (po *customPartialObject) IsNamespaced() bool { return po.namespaced }
func (po *customPartialObject) IsSingleton() bool  { return po.singleton }
