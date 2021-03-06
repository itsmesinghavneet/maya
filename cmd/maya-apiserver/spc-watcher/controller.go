/*
Copyright 2017 The OpenEBS Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package spc

import (
	"github.com/golang/glog"
	apis "github.com/openebs/maya/pkg/apis/openebs.io/v1alpha1"
	clientset "github.com/openebs/maya/pkg/client/clientset/versioned"
	openebsScheme "github.com/openebs/maya/pkg/client/clientset/versioned/scheme"
	informers "github.com/openebs/maya/pkg/client/informers/externalversions"
	corev1 "k8s.io/api/core/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const controllerAgentName = "spc-controller"
const (
	addEvent    = "add"
	updateEvent = "update"
	deleteEvent = "delete"
	ignoreEvent = "ignore"
)

// Controller is the controller implementation for SPC resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface

	// clientset is a openebs custom resource package generated for custom API group.
	clientset clientset.Interface

	// spcSynced is used for caches sync to get populated
	spcSynced cache.InformerSynced

	// deletedIndexer holds deleted resource to be retreived after workqueue
	deletedIndexer cache.Indexer

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController returns a new controller
func NewController(
	kubeclientset kubernetes.Interface,
	clientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	spcInformerFactory informers.SharedInformerFactory) *Controller {
	// obtain references to shared index informers for the SPC resources
	spcInformer := spcInformerFactory.Openebs().V1alpha1().StoragePoolClaims()
	// Create event broadcaster
	// Add new-controller types to the default Kubernetes Scheme so Events can be
	// logged for new-controller types.
	openebsScheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset: kubeclientset,
		clientset:     clientset,
		deletedIndexer: cache.NewIndexer(cache.DeletionHandlingMetaNamespaceKeyFunc,
			cache.Indexers{}),
		spcSynced: spcInformer.Informer().HasSynced,
		workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "SPC"),
		recorder:  recorder,
	}

	glog.Info("Setting up event handlers")
	// Instantiating QueueLoad before pushing it to workqueue.
	q := QueueLoad{}

	// Set up an event handler for when SPC resources change
	spcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			q.Operation = addEvent
			q.Object = obj
			controller.enqueueSpc(obj, q)

		},

		// Informer will send update event along with object in following cases:
		// 1. In case the object is update ( Change of Resource Version)
		// 2. In case the object is deleted
		// 3. After every fixed amount of time which is know as reSync Period.
		//    ReSync period can be set to values we want. It can help in reconiciliation.
		UpdateFunc: func(old, new interface{}) {
			newSpc := new.(*apis.StoragePoolClaim)
			oldSpc := old.(*apis.StoragePoolClaim)
			if newSpc.ObjectMeta.ResourceVersion == oldSpc.ObjectMeta.ResourceVersion {
				// If Resource Version is same it means the object has not got updated.
				q.Operation = ignoreEvent
			} else {
				if IsDeleteEvent(newSpc) {
					q.Operation = deleteEvent
				} else {
					// To-DO
					// Implement Logic for Update of SPC object
					q.Operation = updateEvent
				}
				q.Object = new
				controller.enqueueSpc(new, q)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// obj is the object to be deleted
			// If the use case is to utilize the content of deleted object, a handler should be hooked in here only.
			// Workqueue stores key of object and the object cannot be retrieved later.
			// One of the alternative is to use delete index cache.
		},
	})

	return controller
}

// IsDestroyEvent is to check if the call is for SPC delete.
func IsDeleteEvent(spc *apis.StoragePoolClaim) bool {
	if spc.ObjectMeta.DeletionTimestamp != nil {
		return true
	}
	return false
}
