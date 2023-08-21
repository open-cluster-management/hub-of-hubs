package bundle

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"

	"github.com/stolostron/multicluster-global-hub/pkg/bundle/status"
)

// NewGenericStatusBundle creates a new instance of GenericStatusBundle.
func NewGenericStatusBundle(leafHubName string, manipulateObjFunc func(obj Object)) Bundle {
	if manipulateObjFunc == nil {
		manipulateObjFunc = func(object Object) {
			// do nothing
		}
	}

	return &GenericStatusBundle{
		Objects:           make([]Object, 0),
		LeafHubName:       leafHubName,
		BundleVersion:     status.NewBundleVersion(),
		manipulateObjFunc: manipulateObjFunc,
		lock:              sync.Mutex{},
	}
}

// GenericStatusBundle is a bundle that is used to send to the hub of hubs the leaf CR as is
// except for fields that are not relevant in the hub of hubs like finalizers, etc.
// for bundles that require more specific behavior, it's required to implement your own status bundle struct.
type GenericStatusBundle struct {
	Objects           []Object              `json:"objects"`
	LeafHubName       string                `json:"leafHubName"`
	BundleVersion     *status.BundleVersion `json:"bundleVersion"`
	manipulateObjFunc func(obj Object)
	lock              sync.Mutex
}

// UpdateObject function to update a single object inside a bundle.
func (bundle *GenericStatusBundle) UpdateObject(object Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	bundle.manipulateObjFunc(object)

	index, err := bundle.getObjectIndexByUID(object.GetUID())
	if err != nil { // object not found, need to add it to the bundle
		bundle.Objects = append(bundle.Objects, object)
		bundle.BundleVersion.Incr()
		return
	}

	// if we reached here, object already exists in the bundle. check if we need to update the object
	if object.GetResourceVersion() == bundle.Objects[index].GetResourceVersion() {
		return // update in bundle only if object changed. check for changes using resourceVersion field
	}

	bundle.Objects[index] = object
	bundle.BundleVersion.Incr()
}

// DeleteObject function to delete a single object inside a bundle.
func (bundle *GenericStatusBundle) DeleteObject(object Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	index, err := bundle.getObjectIndexByObj(object)
	if err != nil { // trying to delete object which doesn't exist - return with no error
		return
	}
	bundle.Objects = append(bundle.Objects[:index], bundle.Objects[index+1:]...) // remove from objects
	bundle.BundleVersion.Incr()
}

// GetBundleVersion function to get bundle version.
func (bundle *GenericStatusBundle) GetBundleVersion() *status.BundleVersion {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	return bundle.BundleVersion
}

func (bundle *GenericStatusBundle) getObjectIndexByUID(uid types.UID) (int, error) {
	for i, object := range bundle.Objects {
		if object.GetUID() == uid {
			return i, nil
		}
	}

	return -1, ErrObjectNotFound
}

func (bundle *GenericStatusBundle) getObjectIndexByObj(obj Object) (int, error) {
	if len(obj.GetUID()) > 0 {
		for i, object := range bundle.Objects {
			if object.GetUID() == obj.GetUID() {
				return i, nil
			}
		}
	} else {
		for i, object := range bundle.Objects {
			if object.GetNamespace() == obj.GetNamespace() && object.GetName() == obj.GetName() {
				return i, nil
			}
		}
	}
	return -1, ErrObjectNotFound
}
