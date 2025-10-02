/*
 *  Copyright (c) 2025, WSO2 LLC. (http://www.wso2.org) All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *
 */

package discovery

import (
	"sync"

	"github.com/wso2-extensions/apim-gw-connectors/common-agent/config"
	commonDiscovery "github.com/wso2-extensions/apim-gw-connectors/common-agent/pkg/discovery"
	"github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/loggers"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	configOnce sync.Once
	CRWatcher  *commonDiscovery.CRWatcher
	resourceMutex sync.Mutex
)

// IsControlPlaneInitiated checks if the resource was initiated from control plane
func IsControlPlaneInitiated(u *unstructured.Unstructured) bool {
	if origin, exists := u.GetLabels()[InitiatedFromLabel]; exists {
		return origin == ControlPlaneOrigin
	}
	return false
}

func addResource(u *unstructured.Unstructured) {
	resourceMutex.Lock()
	defer resourceMutex.Unlock()
	loggers.LoggerWatcher.Debugf("EG Resource Added: %s/%s (Kind: %s)", u.GetNamespace(), u.GetName(), u.GetKind())
	if u.GetKind() == HTTPRouteKind && !IsControlPlaneInitiated(u) {
		handleAddOrUpdateHTTPRoute(nil, u)
	} else if u.GetKind() == SecurityPolicyKind && !IsControlPlaneInitiated(u) {
		handleAddOrUpdateSecurityPolicy(u)
	} else if u.GetKind() == BackendTrafficPolicyKind && !IsControlPlaneInitiated(u) {
		handleAddOrUpdateBackendTrafficPolicy(u)
	}
}

func updateResource(oldU, newU *unstructured.Unstructured) {
	resourceMutex.Lock()
	defer resourceMutex.Unlock()
	if oldU.GetResourceVersion() == newU.GetResourceVersion() {
		return
	}
	loggers.LoggerWatcher.Debugf("EG Resource Updated: %s/%s (Kind: %s)", newU.GetNamespace(), newU.GetName(), newU.GetKind())
	if newU.GetKind() == HTTPRouteKind && !IsControlPlaneInitiated(newU) {
		handleAddOrUpdateHTTPRoute(oldU, newU)
	} else if newU.GetKind() == SecurityPolicyKind && !IsControlPlaneInitiated(newU) {
		handleAddOrUpdateSecurityPolicy(newU)
	} else if newU.GetKind() == BackendTrafficPolicyKind && !IsControlPlaneInitiated(newU) {
		handleAddOrUpdateBackendTrafficPolicy(newU)
	}
}

func deleteResource(u *unstructured.Unstructured) {
	resourceMutex.Lock()
	defer resourceMutex.Unlock()
	loggers.LoggerWatcher.Debugf("EG Resource Deleted: %s/%s (Kind: %s)", u.GetNamespace(), u.GetName(), u.GetKind())
	if u.GetKind() == HTTPRouteKind && !IsControlPlaneInitiated(u) {
		handleDeleteHTTPRoute(u)
	} else if u.GetKind() == SecurityPolicyKind && !IsControlPlaneInitiated(u) {
		handleDeleteSecurityPolicy(u)
	} else if u.GetKind() == BackendTrafficPolicyKind && !IsControlPlaneInitiated(u) {
		handleDeleteBackendTrafficPolicy(u)
	}
}

func init() {
	configOnce.Do(func() {
		conf, _ := config.ReadConfigs()
		CRWatcher = &commonDiscovery.CRWatcher{
			Namespace:     conf.DataPlane.Namespace,
			GroupVersions: []schema.GroupVersionResource{HTTPRouteGVR, SecurityPolicyGVR, BackendTrafficPolicyGVR},
			AddFunc:       addResource,
			UpdateFunc:    updateResource,
			DeleteFunc:    deleteResource,
		}
	})
}
