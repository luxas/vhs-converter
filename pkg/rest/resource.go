package rest

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/labstack/echo"
	"github.com/luxas/digitized/pkg/apis/meta"
	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const nameParamPath = "/:name/"

type (
	// ResourceHookHandler is a hook handler that operates in a resource context.
	ResourceHookHandler func(ResourceContext) error
	// NamedResourceHookHandler is a hook handler that operates in a named resource context.
	NamedResourceHookHandler func(NamedResourceContext) error
	// ObjectHookHandler is a hook handler that operates in resource context, where there
	// is JSON data in the body that has been is decoded. The hook executes before data is
	// saved to the storage though, so obj is a pointer that can be mutated.
	ObjectHookHandler func(rc ResourceContext, obj client.Object) error
)

func newResourceHandler(parent *echo.Group, gvkr GroupVersionKindResource) ResourceHandler {
	rh := &resourceHandlerImpl{nil, gvkr, nil, nil, nil}

	resourcePath := "/" + gvkr.Resource
	if gvkr.Namespaced {
		// List resources in all namespaces
		parent.GET(resourcePath+"/", rh.list, resourceContextMiddleware(gvkr))
		// Add the namespaced resource route
		rh.g = parent.Group("/namespaces/:namespace"+resourcePath, resourceContextMiddleware(gvkr), namespacedResourceContextMiddleware())
	} else {
		rh.g = parent.Group(resourcePath, resourceContextMiddleware(gvkr))
	}

	// Register the relevant routes
	rh.g.POST("/", rh.create)
	// TODO: PUT should be named?
	rh.g.PUT("/", rh.update)
	// Singletons don't have named routes, nor LIST
	if gvkr.Singleton {
		// TODO: Test if/how well namespaced singletons function
		rh.g.GET("/", rh.get, namedResourceContextMiddleware())
		rh.g.PATCH("/", rh.patch, namedResourceContextMiddleware())
	} else {
		rh.g.GET("/", rh.list)
		rh.g.GET(nameParamPath, rh.get, namedResourceContextMiddleware())
		rh.g.PATCH(nameParamPath, rh.patch, namedResourceContextMiddleware())
		rh.g.DELETE(nameParamPath, rh.delete, namedResourceContextMiddleware())
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
	rh.g.PUT(nameParamPath+name+"/", func(c echo.Context) error {
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

func (rh *resourceHandlerImpl) validateBody(rc ResourceContext, obj client.Object) error {
	// Enforce that the namespaces match
	if ns, ok := rc.(NamespacedResourceContext); ok && ns.Namespace() != obj.GetNamespace() {
		return fmt.Errorf("Object namespace in body: %q doesn't match namespace in path: %q", obj.GetNamespace(), ns.Namespace())
	}
	// Validate the object if possible
	return meta.ValidateIfPossible(obj)
}

func (rh *resourceHandlerImpl) returnFromStorage(ctx context.Context, rc ResourceContext, obj storage.Object) error {
	err := rc.Storage().Get(ctx, client.ObjectKeyFromObject(obj), obj)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return newStatusError(http.StatusNotFound, storage.ErrNotFound)
		}
		return err
	}
	return rc.JSONIndent(http.StatusOK, obj)
}

func (rh *resourceHandlerImpl) list(c echo.Context) error {
	rc := c.(ResourceContext)
	ctx := context.Background()
	// Conditionally filter on namespace
	var listOpts []client.ListOption
	if ns, ok := rc.(NamespacedResourceContext); ok {
		listOpts = append(listOpts, client.InNamespace(ns.Namespace()))
	}
	// TODO: Move to typed lists?
	list := &unstructured.UnstructuredList{}
	rc.ApplyTypeMetaToList(list)
	err := rc.Storage().List(ctx, list, listOpts...)
	if err != nil {
		return err
	}

	return rc.JSONIndent(http.StatusOK, list)
}

func (rh *resourceHandlerImpl) decodeObject(rc ResourceContext) (client.Object, error) {
	// Decode the body and cast it to libgitops runtime.Object
	kobj, err := rc.Storage().Serializer().Decoder().Decode(serializer.NewJSONFrameReader(rc.Request().Body))
	if err != nil {
		return nil, newStatusError(http.StatusBadRequest, err)
	}
	obj, ok := kobj.(client.Object)
	if !ok {
		return nil, newStatusErrorf(http.StatusInternalServerError, "couldn't cast decoded object to runtime.Object")
	}

	// Validate the body
	if err := rh.validateBody(rc, obj); err != nil {
		return nil, newStatusError(http.StatusBadRequest, err)
	}
	return obj, nil
}

func (rh *resourceHandlerImpl) create(c echo.Context) error {
	rc := c.(ResourceContext)
	ctx := context.Background()

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
	if err := rc.Storage().Create(ctx, obj); err != nil {
		// Return BadRequest if the resource already exists
		if errors.Is(err, storage.ErrAlreadyExists) {
			return newStatusError(http.StatusBadRequest, storage.ErrAlreadyExists)
		}
		return newStatusError(http.StatusInternalServerError, err)
	}

	return rc.JSONIndent(http.StatusCreated, obj)
}

func (rh *resourceHandlerImpl) patch(c echo.Context) error {
	rc := c.(NamedResourceContext)
	ctx := context.Background()

	// We need to read the patch twice, once for getting the ObjectKey info
	patch, err := ioutil.ReadAll(rc.Request().Body)
	if err != nil {
		return err
	}
	rc.Request().Body.Close()

	// TODO: Verify that GVKR & decoded object GVK match,
	// or do we validate the GVK in the patch? Does k8s?
	// TODO: Use the same logic as PUT underneath, to make use of validateBody

	// The patch type will be validated in the storage
	// TODO: Maybe remove "; charset=" if included in header like
	// https://github.com/kubernetes/apiserver/blob/v0.20.1/pkg/endpoints/handlers/patch.go#L61
	patchType := types.PatchType(rc.Request().Header.Get("Content-Type"))

	obj, err := rc.NewNamedObject()
	if err != nil {
		return err
	}

	// TODO: This is a hack, but workaround that ObjectMeta is not serialized at the moment
	// This is needed to make sure .metadata.name is set for the underlying storage.
	// TODO: This should be worked around so that there is a dedicated singleton middleware for NamedResourceContext
	if rh.gvkr.Singleton {
		rc.ApplyName(obj)
	}

	// Write the patch to the underlying storage
	// TODO: Make sure we validate the new payload here before applying.
	// Maybe it would be possible for patch not to actually apply the change to
	// RawStorage, but just write the updates to obj?
	if err := rc.Storage().Patch(ctx, obj, client.RawPatch(patchType, patch)); err != nil {
		return err
	}
	// After doing the patch, return the new object from storage
	return rc.JSONIndent(200, obj)
}

func (rh *resourceHandlerImpl) update(c echo.Context) error {
	rc := c.(ResourceContext)
	ctx := context.Background()

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
	// TODO: storage.GVKForObject should check for obj==nil
	if err := rc.Storage().Update(ctx, obj); err != nil {
		return err
	}

	return rc.JSONIndent(http.StatusOK, obj)
}

func (rh *resourceHandlerImpl) get(c echo.Context) error {
	rc := c.(NamedResourceContext)
	ctx := context.Background()

	obj, err := rc.NewNamedObject()
	if err != nil {
		return err
	}
	return rh.returnFromStorage(ctx, rc, obj)
}

func (rh *resourceHandlerImpl) delete(c echo.Context) error {
	rc := c.(NamedResourceContext)
	ctx := context.Background()

	// Run all hooks in order
	for _, hook := range rh.deleteHooks {
		if err := hook(rc); err != nil {
			return err
		}
	}

	obj, err := rc.NewNamedObject()
	if err != nil {
		return err
	}
	if err := rc.Storage().Delete(ctx, obj); err != nil {
		return err
	}
	return rc.NoContent(http.StatusNoContent)
}
