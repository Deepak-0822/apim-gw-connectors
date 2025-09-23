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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/wso2-extensions/apim-gw-connectors/common-agent/config"
	commonDiscovery "github.com/wso2-extensions/apim-gw-connectors/common-agent/pkg/discovery"
	"github.com/wso2-extensions/apim-gw-connectors/common-agent/pkg/managementserver"
	"github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/loggers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// handleAddOrUpdateHTTPRoute processes create/update for a single HTTPRoute
func handleAddOrUpdateHTTPRoute(oldU, u *unstructured.Unstructured) {
	loggers.LoggerWatcher.Infof("EG Processing HTTPRoute: %s/%s (rv: %s)", u.GetNamespace(), u.GetName(), u.GetResourceVersion())
	// Compute hash to skip no-op updates
	if oldU != nil && generateRouteHash(oldU) == generateRouteHash(u) {
		loggers.LoggerWatcher.Debugf("EG HTTPRoute %s/%s unchanged, skipping", u.GetNamespace(), u.GetName())
		return
	}

	// Determine label keys from env
	apiNameLabel := getEnvOrDefault(EnvLabelAPINameKey, DefaultAPINameLabel)
	apiSpecLabel := getEnvOrDefault(EnvLabelAPISpecKey, DefaultAPISpecLabel)

	// Find the group of routes to process (by shared apiNameLabel)
	routes := []*unstructured.Unstructured{u}
	if val, ok := u.GetLabels()[apiNameLabel]; ok && val != EmptyString {
		routes = listHTTPRoutesByLabel(u.GetNamespace(), apiNameLabel, val)
	}

	// If any route in group has the API spec label, fetch spec from ConfigMap
	var definition string
	for _, r := range routes {
		if cmName, found := r.GetLabels()[apiSpecLabel]; found && cmName != EmptyString {
			if def := fetchOpenAPISpecFromConfigMap(r.GetNamespace(), cmName); def != EmptyString {
				definition = def
				break
			}
		}
	}

	// Else generate from routes
	if definition == EmptyString {
		conf, _ := config.ReadConfigs()
		apiUUID := uuid.New().String()
		defObj, err := commonDiscovery.GenerateOpenAPIDefinition(routes, apiUUID, conf)
		if err != nil {
			loggers.LoggerWatcher.Errorf("EG failed to generate OpenAPI for routes: %v", err)
			return
		}
		data, _ := json.Marshal(defObj)
		definition = string(data)
	}

	// Build desired API payload. Name and version derived from labels
	apiName := deriveAPIName(routes, apiNameLabel)
	apiVersion := deriveAPIVersion(routes)
	apiUUID := computeStableUUID(apiName, apiVersion, routes)

	api := managementserver.API{
		APIUUID:          apiUUID,
		APIName:          apiName,
		APIVersion:       apiVersion,
		IsDefaultVersion: true,
		APIType:          "rest",
		Definition:       definition,
	}

	// Extract operations and vhost/basePath
	updateAPIFromHTTPRoutes(&api, routes)

	// Hash and queue if changed
	newHash := computeAPIHash(api)
	oldHash, exists := commonDiscovery.APIHashMap[apiUUID]
	if !exists || oldHash != newHash {
		commonDiscovery.APIHashMap[apiUUID] = newHash
		commonDiscovery.APIMap[apiUUID] = api
		commonDiscovery.QueueEvent(managementserver.CreateEvent, api, apiName, routes[0].GetNamespace(), "EG", apiUUID)
		loggers.LoggerWatcher.Infof("EG queued API upsert: %s (%s)", apiName, apiUUID)
	} else {
		loggers.LoggerWatcher.Debugf("EG API unchanged: %s", apiUUID)
	}
}

func listHTTPRoutesByLabel(namespace, key, value string) []*unstructured.Unstructured {
	sel := fmt.Sprintf("%s=%s", key, value)
	list, err := CRWatcher.DynamicClient.Resource(HTTPRouteGVR).Namespace(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		loggers.LoggerWatcher.Errorf("EG list HTTPRoutes by label failed: %v", err)
		return []*unstructured.Unstructured{}
	}
	var out []*unstructured.Unstructured
	for i := range list.Items {
		r := list.Items[i]
		if !IsControlPlaneInitiated(&r) {
			out = append(out, &r)
		}
	}
	return out
}

func fetchOpenAPISpecFromConfigMap(namespace, name string) string {
	cm, err := CRWatcher.DynamicClient.Resource(ConfigMapGVR).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		loggers.LoggerWatcher.Errorf("EG fetch ConfigMap %s/%s failed: %v", namespace, name, err)
		return EmptyString
	}
	key := getEnvOrDefault(EnvConfigMapKeyNameKey, DefaultConfigMapKeyName)
	if data, found, _ := unstructured.NestedStringMap(cm.Object, DataField); found {
		if v, ok := data[key]; ok {
			return v
		}
	}
	return EmptyString
}

func deriveAPIName(routes []*unstructured.Unstructured, apiNameLabel string) string {
	// Prefer label value, else fallback to first route name
	for _, r := range routes {
		if v, ok := r.GetLabels()[apiNameLabel]; ok && v != EmptyString {
			return v
		}
	}
	if len(routes) > 0 {
		return routes[0].GetName()
	}
	return "eg-api"
}

func deriveAPIVersion(routes []*unstructured.Unstructured) string {
	// Try label apiVersion, else default
	for _, r := range routes {
		if v, ok := r.GetLabels()["apiVersion"]; ok && v != EmptyString {
			return v
		}
	}
	return "v1"
}

func updateAPIFromHTTPRoutes(api *managementserver.API, routes []*unstructured.Unstructured) {
	for _, u := range routes {
		// Convert unstructured -> typed HTTPRoute
		var httpRoute gatewayv1.HTTPRoute
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &httpRoute)
		if err != nil {
			loggers.LoggerWatcher.Errorf("EG failed to convert unstructured to HTTPRoute: %v", err)
			continue
		}

		// --- Hostnames ---
		if len(httpRoute.Spec.Hostnames) > 0 {
			h := string(httpRoute.Spec.Hostnames[0])
			if h != "" && api.Vhost == "" {
				api.Vhost = h
			}
		}

		// --- Rules & Matches ---
		for _, rule := range httpRoute.Spec.Rules {
			// Set production endpoint from BackendRef (EnvoyGateway convention)
			if len(rule.BackendRefs) > 0 {
				// Only use the first backendRef for production endpoint
				backendRef := rule.BackendRefs[0]
				// EnvoyGateway expects BackendRef to have Name, Namespace, Port
				// Build endpoint URL (assuming HTTP for now)
				endpointHost := backendRef.Name
				endpointPort := "80"
				if backendRef.Port != nil {
					endpointPort = fmt.Sprintf("%d", *backendRef.Port)
				}
				// If Namespace is set, use it (optional)
				endpointNamespace := httpRoute.Namespace
				if backendRef.Namespace != nil && *backendRef.Namespace != "" {
					endpointNamespace = string(*backendRef.Namespace)
				}
				// Compose endpoint URL (K8s DNS format)
				prodEndpoint := fmt.Sprintf("http://%s.%s.svc.cluster.local:%s", endpointHost, endpointNamespace, endpointPort)
				api.ProdEndpoint = prodEndpoint
			}
			for _, match := range rule.Matches {
				// Path
				p := "/"
				if match.Path != nil && match.Path.Value != nil {
					p = *match.Path.Value
				}

				// Method
				method := DefaultHTTPMethod
				if match.Method != nil {
					method = string(*match.Method)
					if method == "" {
						method = DefaultHTTPMethod
					} else {
						method = strings.ToUpper(method)
					}
				}

				op := managementserver.OperationFromDP{
					Path:   p,
					Verb:   method,
					Scopes: []string{},
				}
				if !operationExists(api.Operations, op) {
					api.Operations = append(api.Operations, op)
				}
			}
		}
	}

	// Derive base path
	api.BasePath = extractBasePathFromOperations(api.Operations)
	if api.BasePath == "/" {
		api.BasePath = fmt.Sprintf("/%s", api.APIName)
	}
}

func extractBasePathFromOperations(ops []managementserver.OperationFromDP) string {
	if len(ops) == 0 {
		return "/"
	}
	var paths []string
	for _, op := range ops {
		paths = append(paths, op.Path)
	}
	sort.Strings(paths)
	prefix := paths[0]
	for _, p := range paths[1:] {
		for !strings.HasPrefix(p, prefix) && prefix != EmptyString {
			prefix = prefix[:len(prefix)-1]
		}
		if prefix == EmptyString {
			return "/"
		}
	}
	parts := strings.Split(prefix, "/")
	if len(parts) > 1 {
		return "/" + parts[1]
	}
	return prefix
}

func operationExists(ops []managementserver.OperationFromDP, newOp managementserver.OperationFromDP) bool {
	for _, op := range ops {
		if op.Path == newOp.Path && op.Verb == newOp.Verb {
			return true
		}
	}
	return false
}

func computeAPIHash(api managementserver.API) string {
	data := fmt.Sprintf("%v%v%v%v%v", api.APIName, api.APIVersion, api.Definition, api.Vhost, api.Operations)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func computeStableUUID(name, version string, routes []*unstructured.Unstructured) string {
	// Deterministic UUID based on name+version
	data := name + "|" + version
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func generateRouteHash(route *unstructured.Unstructured) string {
	if route == nil {
		return EmptyString
	}
	spec := route.Object[SpecField]
	// Only hash spec and relevant labels
	labels := route.GetLabels()
	relevant := map[string]interface{}{"spec": spec}
	if labels != nil {
		if v, ok := labels[getEnvOrDefault(EnvLabelAPINameKey, DefaultAPINameLabel)]; ok {
			relevant["apiNameLabel"] = v
		}
		if v, ok := labels[getEnvOrDefault(EnvLabelAPISpecKey, DefaultAPISpecLabel)]; ok {
			relevant["apiSpecLabel"] = v
		}
	}
	b, _ := json.Marshal(relevant)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != EmptyString {
		return v
	}
	return def
}

// handleDeleteHTTPRoute processes HTTPRoute deletion semantics
func handleDeleteHTTPRoute(deleted *unstructured.Unstructured) {
	loggers.LoggerWatcher.Infof("EG Processing HTTPRoute delete: %s/%s", deleted.GetNamespace(), deleted.GetName())

	apiNameLabel := getEnvOrDefault(EnvLabelAPINameKey, DefaultAPINameLabel)
	apiName, has := deleted.GetLabels()[apiNameLabel]
	if has && apiName != EmptyString {
		// Check for remaining routes in the group
		remaining := listHTTPRoutesByLabel(deleted.GetNamespace(), apiNameLabel, apiName)
		if len(remaining) > 0 {
			// Reconcile with remaining routes: trigger add/update path using the first remaining route
			loggers.LoggerWatcher.Infof("EG found %d remaining HTTPRoutes for API '%s' after delete; re-reconciling", len(remaining), apiName)
			handleAddOrUpdateHTTPRoute(nil, remaining[0])
			return
		}
		// No routes remain, delete API from CP
		apiVersion := deriveAPIVersion([]*unstructured.Unstructured{deleted})
		apiUUID := computeStableUUID(apiName, apiVersion, []*unstructured.Unstructured{deleted})
		if api, ok := commonDiscovery.APIMap[apiUUID]; ok {
			delete(commonDiscovery.APIMap, apiUUID)
			delete(commonDiscovery.APIHashMap, apiUUID)
			commonDiscovery.QueueEvent(managementserver.DeleteEvent, api, apiName, deleted.GetNamespace(), "EG", apiUUID)
			loggers.LoggerWatcher.Infof("EG queued API delete: %s (%s)", apiName, apiUUID)
		} else {
			loggers.LoggerWatcher.Debugf("EG no API found for delete request: %s (%s)", apiName, apiUUID)
		}
		return
	}

	// If no API name label, treat the deleted route as a single-route API; recompute UUID with its own name
	apiName = deleted.GetName()
	apiVersion := deriveAPIVersion([]*unstructured.Unstructured{deleted})
	apiUUID := computeStableUUID(apiName, apiVersion, []*unstructured.Unstructured{deleted})
	if api, ok := commonDiscovery.APIMap[apiUUID]; ok {
		delete(commonDiscovery.APIMap, apiUUID)
		delete(commonDiscovery.APIHashMap, apiUUID)
		commonDiscovery.QueueEvent(managementserver.DeleteEvent, api, apiName, deleted.GetNamespace(), "EG", apiUUID)
		loggers.LoggerWatcher.Infof("EG queued single-route API delete: %s (%s)", apiName, apiUUID)
	}
}
