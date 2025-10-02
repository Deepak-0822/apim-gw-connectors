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

	gatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/google/uuid"
	"github.com/wso2-extensions/apim-gw-connectors/common-agent/config"
	commonDiscovery "github.com/wso2-extensions/apim-gw-connectors/common-agent/pkg/discovery"
	"github.com/wso2-extensions/apim-gw-connectors/common-agent/pkg/managementserver"
	"github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/constants"
	"github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/loggers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwapiv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// handleAddOrUpdateHTTPRoute processes create/update for a single HTTPRoute
func handleAddOrUpdateHTTPRoute(oldU, newU *unstructured.Unstructured) {
	var httpRoute gatewayv1.HTTPRoute
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(newU.Object, &httpRoute)
	if err != nil {
		// handle error
		loggers.LoggerWatcher.Errorf("Failed to convert unstructured to HTTPRoute: %v", err)
		return
	}
	httpRouteStatusOk := true
	for _, parent := range httpRoute.Status.Parents {
		for _, cond := range parent.Conditions {
			if cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != httpRoute.Generation {
				loggers.LoggerWatcher.Infof("HTTPRoute %s/%s is not accepted or the latest is not processed yet by gateway. Observed generation: %d, Current generation: %d", newU.GetNamespace(), newU.GetName(), cond.ObservedGeneration, httpRoute.Generation)
				httpRouteStatusOk = false
				break
			}
		}
	}
	if !httpRouteStatusOk {
		loggers.LoggerWatcher.Infof("EG HTTPRoute %s/%s is not accepted by gateway", newU.GetNamespace(), newU.GetName())
		return
	}
	loggers.LoggerWatcher.Infof("EG Processing HTTPRoute: %s/%s (rv: %s)", newU.GetNamespace(), newU.GetName(), newU.GetResourceVersion())
	// Compute hash to skip no-op updates
	if oldU != nil && generateRouteHash(oldU) == generateRouteHash(newU) {
		loggers.LoggerWatcher.Debugf("EG HTTPRoute %s/%s unchanged, skipping", newU.GetNamespace(), newU.GetName())
		return
	}

	// Determine label keys from env
	apiNameLabel := getEnvOrDefault(EnvLabelAPINameKey, DefaultAPINameLabel)
	apiSpecLabel := getEnvOrDefault(EnvLabelAPISpecKey, DefaultAPISpecLabel)

	// Find the group of routes to process (by shared apiNameLabel)
	routes := []*unstructured.Unstructured{newU}
	if val, ok := newU.GetLabels()[apiNameLabel]; ok && val != EmptyString {
		loggers.LoggerWatcher.Debugf("EG grouping HTTPRoutes by label %s=%s", apiNameLabel, val)
		routes = listHTTPRoutesByLabel(newU.GetNamespace(), apiNameLabel, val)
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
	apiUUID, _ := getLabelValue(newU, constants.LabelAPIUUID)

	api := managementserver.API{
		APIName:          apiName,
		APIVersion:       apiVersion,
		IsDefaultVersion: true,
		APIType:          "rest",
		Definition:       definition,
	}
	
	// Extract operations and vhost/basePath
	updateAPIFromHTTPRoutes(&api, routes)

	crsToHash := []*unstructured.Unstructured{}
	crsToHash = append(crsToHash, routes...)
	sps, btps, err := getPoliciesTargetingHTTPRoute(newU)
	if err != nil {
		loggers.LoggerWatcher.Errorf("EG failed to get policies targeting HTTPRoute %s/%s: %v", newU.GetNamespace(), newU.GetName(), err)
	} else {
		if len(sps) > 0 {
			loggers.LoggerWatcher.Infof("Found %d SecurityPolicies targeting HTTPRoute %s/%s", len(sps), newU.GetNamespace(), newU.GetName())
			crsToHash = append(crsToHash, sps...)
		}
		if len(btps) > 0 {
			loggers.LoggerWatcher.Infof("Found %d BackendTrafficPolicies targeting HTTPRoute %s/%s", len(btps), newU.GetNamespace(), newU.GetName())
			crsToHash = append(crsToHash, btps...)
		}
	}
	// Hash and queue if changed
	newHash := computeAPIHash(crsToHash)
	
	loggers.LoggerWatcher.Debugf("EG API hash unchanged: newHash=%s", newHash)
	queue := false
	if val, ok := newU.GetLabels()[constants.LabelAPIHash]; ok && val != newHash {
		loggers.LoggerWatcher.Debugf("EG API hash changed: oldHash=%s, newHash=%s", val, newHash)
		queue = true
	} else if !ok {
		loggers.LoggerWatcher.Debugf("EG API hash missing, setting newHash=%s", newHash)
		queue = true
	}
	if queue && !cache.Exists(newHash) {
		cache.Add(newHash)
		agentName := constants.GetAgentName()
		commonDiscovery.QueueEvent(managementserver.CreateEvent, api, routes[0].GetName(), routes[0].GetNamespace(), agentName, apiUUID)
		loggers.LoggerWatcher.Infof("EG queued API upsert: %s (%s)", apiName, apiUUID)
	} else {
		loggers.LoggerWatcher.Debugf("EG API unchanged: %s", apiUUID)
	}
}

func listHTTPRoutesByLabel(namespace, key, value string) []*unstructured.Unstructured {
	sel := fmt.Sprintf("%s=%s", key, value)
	loggers.LoggerWatcher.Debugf("EG listing HTTPRoutes in %s with selector: %s", namespace, sel)
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
	loggers.LoggerWatcher.Debugf("EG fetching OpenAPI spec from ConfigMap %s/%s", namespace, name)
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
		secured := false
		rlID := ""
		scopes := []string{}
		sps, btps, errP := getPoliciesTargetingHTTPRoute(u)
		if errP != nil {
			loggers.LoggerWatcher.Errorf("EG failed to get policies targeting HTTPRoute: %v", errP)
		}
		if len(sps) > 0 {
			sp := sps[0]
			// Convert unstructured SecurityPolicy to typed Envoy Gateway object
			var egSP gatewayv1alpha1.SecurityPolicy
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(sp.Object, &egSP); err != nil {
				loggers.LoggerWatcher.Errorf("EG failed to convert unstructured to SecurityPolicy: %v", err)
			} else {
				loggers.LoggerWatcher.Debugf("EG converted SecurityPolicy to typed object: %s/%s", egSP.Namespace, egSP.Name)
				if egSP.Spec.JWT != nil && len(egSP.Spec.JWT.Providers) > 0 {
					secured = true
				}
				if egSP.Spec.Authorization != nil && len(egSP.Spec.Authorization.Rules) > 0 {
					for _, rule := range egSP.Spec.Authorization.Rules {
						if rule.Principal.JWT != nil && len(rule.Principal.JWT.Scopes) > 0 {
							for _, s := range rule.Principal.JWT.Scopes {
								scopes = append(scopes, string(s))
							}
						}
					}
				}
			}
		}
		
		// remove duplicates from scopes
		scopeSet := make(map[string]struct{})
		for _, s := range scopes {
			scopeSet[s] = struct{}{}
		}
		scopes = scopes[:0]
		for s := range scopeSet {
			scopes = append(scopes, s)
		}
		if len(btps) > 0 {
			btp := btps[0]
			// Convert unstructured BackendTrafficPolicy to typed Envoy Gateway object
			var egBTP gatewayv1alpha1.BackendTrafficPolicy
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(btp.Object, &egBTP); err != nil {
				loggers.LoggerWatcher.Errorf("EG failed to convert unstructured to BackendTrafficPolicy: %v", err)
			} else {
				loggers.LoggerWatcher.Debugf("EG converted BackendTrafficPolicy to typed object: %s/%s", egBTP.Namespace, egBTP.Name)
				if rateLimitID, ok := getRateLimitIdentifier(&egBTP); ok {
					rlID = rateLimitID
				}
			}
		}
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
				prodEndpoint := fmt.Sprintf("%s.%s.svc.cluster.local:%s", endpointHost, endpointNamespace, endpointPort)
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
					Scopes: scopes,
					Secured: secured,
					RatelimitConfigurationID: rlID,
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

func computeAPIHash(crs []*unstructured.Unstructured) string {
	crIds := make([]string, 0, len(crs))
	for _, r := range crs {
		if r == nil {
			continue
		}
		id := fmt.Sprintf("%s_%d_%s", r.GetName(), r.GetGeneration(), r.GetUID())
		crIds = append(crIds, id)
	}
	sort.Strings(crIds)
	data := strings.Join(crIds, "|")
	sum := sha256.Sum256([]byte(data))
	fullHash := hex.EncodeToString(sum[:])

	// Truncate to max 63 characters for Kubernetes label
	if len(fullHash) > 63 {
		return fullHash[:63]
	}
	return fullHash
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
		apiUUID, _ := getLabelValue(deleted, constants.LabelAPIUUID)
		revisionID := deleted.GetLabels()[constants.LabelRevisionID]
		hostnames, _, _ := unstructured.NestedStringSlice(deleted.Object, "spec", "hostnames")
		hostname := ""
		if len(hostnames) > 0 {
			hostname = hostnames[0]
		}
		api := managementserver.API{
			APIUUID:    apiUUID,
			APIName:    apiName,
			APIVersion: apiVersion,
			RevisionID: revisionID,
			Vhost:      hostname,
		}
		loggers.LoggerWatcher.Infof("EG prepared API for delete: %+v", api)
		agentName := constants.GetAgentName()
		commonDiscovery.QueueEvent(managementserver.DeleteEvent, api, apiName, deleted.GetNamespace(), agentName, apiUUID)
		loggers.LoggerWatcher.Infof("EG queued API delete: %s (%s)", apiName, apiUUID)
		return
	}

	// If no API name label, treat the deleted route as a single-route API; recompute UUID with its own name
	apiName = deleted.GetName()
	apiUUID, _ := getLabelValue(deleted, constants.LabelAPIUUID)
	apiRevisionId, _ := deleted.GetLabels()[constants.LabelRevisionID]
	hostnames, _, _ := unstructured.NestedStringSlice(deleted.Object, "spec", "hostnames")
	hostname := ""
	if len(hostnames) > 0 {
		hostname = hostnames[0]
	}
	api := managementserver.API{
		APIUUID:    apiUUID,
		APIName:    apiName,
		APIVersion: deriveAPIVersion([]*unstructured.Unstructured{deleted}),
		RevisionID: apiRevisionId,
		Vhost:      hostname,
	}
	agentName := constants.GetAgentName()
	commonDiscovery.QueueEvent(managementserver.DeleteEvent, api, apiName, deleted.GetNamespace(), agentName, apiUUID)

}

func getHttpRouteByNameAndNamespace(name, namespace string) (*unstructured.Unstructured, error) {
	if CRWatcher == nil || CRWatcher.DynamicClient == nil {
		return nil, fmt.Errorf("CRWatcher or DynamicClient is not initialized")
	}

	loggers.LoggerWatcher.Debugf("EG fetching HTTPRoute %s/%s", namespace, name)
	httpRoute, err := CRWatcher.DynamicClient.Resource(HTTPRouteGVR).Namespace(namespace).Get(
		context.Background(),
		name,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, err
	}
	return httpRoute, nil
}

func getAllRelatedHttpRoutes(name, namespace string) ([]*unstructured.Unstructured, error) {
	route, err := getHttpRouteByNameAndNamespace(name, namespace)
	if err != nil {
		return nil, err
	}

	apiNameLabel := getEnvOrDefault(EnvLabelAPINameKey, DefaultAPINameLabel)
	apiName, has := route.GetLabels()[apiNameLabel]
	if has && apiName != EmptyString {
		loggers.LoggerWatcher.Debugf("EG finding related HTTPRoutes by %s=%s", apiNameLabel, apiName)
		routes := listHTTPRoutesByLabel(namespace, apiNameLabel, apiName)
		return routes, nil
	}
	return []*unstructured.Unstructured{route}, nil
}

// getPoliciesTargetingHTTPRoute returns all SecurityPolicies and BackendTrafficPolicies that
// reference the given HTTPRoute via spec.policyTargetReferences.targetRefs.
// It searches within the route's namespace.
func getPoliciesTargetingHTTPRoute(route *unstructured.Unstructured) (
	securityPolicies []*unstructured.Unstructured,
	backendTrafficPolicies []*unstructured.Unstructured,
	err error,
) {
	if route == nil {
		return nil, nil, fmt.Errorf("route is nil")
	}
	if CRWatcher == nil || CRWatcher.DynamicClient == nil {
		return nil, nil, fmt.Errorf("CRWatcher or DynamicClient is not initialized")
	}

	ns := route.GetNamespace()

	// List SecurityPolicies in the route's namespace
	spList, spErr := CRWatcher.DynamicClient.Resource(SecurityPolicyGVR).Namespace(ns).List(
		context.Background(), metav1.ListOptions{},
	)
	if spErr != nil {
		err = fmt.Errorf("failed to list SecurityPolicies in %s: %w", ns, spErr)
	} else {
		for i := range spList.Items {
			p := spList.Items[i]
			if policyTargetsHTTPRoute(&p, route) && isPolicyAccepted(&p) {
				cp := p // capture
				securityPolicies = append(securityPolicies, &cp)
			}
		}
	}

	// List BackendTrafficPolicies in the route's namespace
	btpList, btpErr := CRWatcher.DynamicClient.Resource(BackendTrafficPolicyGVR).Namespace(ns).List(
		context.Background(), metav1.ListOptions{},
	)
	if btpErr != nil {
		if err != nil {
			// aggregate
			err = fmt.Errorf("%v; and failed to list BackendTrafficPolicies in %s: %w", err, ns, btpErr)
		} else {
			err = fmt.Errorf("failed to list BackendTrafficPolicies in %s: %w", ns, btpErr)
		}
	} else {
		for i := range btpList.Items {
			p := btpList.Items[i]
			if policyTargetsHTTPRoute(&p, route) && isPolicyAccepted(&p) {
				cp := p // capture
				backendTrafficPolicies = append(backendTrafficPolicies, &cp)
			}
		}
	}

	return securityPolicies, backendTrafficPolicies, err
}

// policyTargetsHTTPRoute checks whether a policy (SecurityPolicy/BackendTrafficPolicy)
// targets the given HTTPRoute using LocalPolicyTargetReference semantics.
func policyTargetsHTTPRoute(policy *unstructured.Unstructured, route *unstructured.Unstructured) bool {
	if policy == nil || route == nil {
		return false
	}

	routeName := route.GetName()
	routeNS := route.GetNamespace()
	policyNS := policy.GetNamespace()

	switch policy.GetKind() {
	case "SecurityPolicy":
		var sp gatewayv1alpha1.SecurityPolicy
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(policy.Object, &sp); err != nil {
			return false
		}
		if sp.Spec.PolicyTargetReferences.TargetRef != nil {
			// Single targetRef case
			return matchTargetRefs([]gwapiv1a2.LocalPolicyTargetReferenceWithSectionName{*sp.Spec.PolicyTargetReferences.TargetRef}, routeName, routeNS, policyNS)
		}
		return matchTargetRefs(sp.Spec.PolicyTargetReferences.TargetRefs, routeName, routeNS, policyNS)

	case "BackendTrafficPolicy":
		var bp gatewayv1alpha1.BackendTrafficPolicy
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(policy.Object, &bp); err != nil {
			return false
		}
		if bp.Spec.PolicyTargetReferences.TargetRef != nil {
			// Single targetRef case
			return matchTargetRefs([]gwapiv1a2.LocalPolicyTargetReferenceWithSectionName{*bp.Spec.PolicyTargetReferences.TargetRef}, routeName, routeNS, policyNS)
		}
		return matchTargetRefs(bp.Spec.PolicyTargetReferences.TargetRefs, routeName, routeNS, policyNS)
	}

	return false
}

// matchTargetRefs checks if any of the targetRefs match the given route.
func matchTargetRefs(refs []gwapiv1a2.LocalPolicyTargetReferenceWithSectionName, routeName, routeNS, policyNS string) bool {
	for _, ref := range refs {
		// Match kind/group for HTTPRoute
		if !strings.EqualFold(string(ref.Kind), HTTPRouteKind) {
			continue
		}
		if ref.Group != "" && string(ref.Group) != HTTPRouteGVR.Group {
			continue
		}
		if string(ref.Name) != routeName {
			continue
		}

		return true
	}
	return false
}

// patchLabelsToHTTPRoute updates the labels of an HTTPRoute resource
func patchLabelsToHTTPRoute(route *unstructured.Unstructured, labels map[string]string) error {
	if CRWatcher == nil || CRWatcher.DynamicClient == nil {
		return fmt.Errorf("CRWatcher or DynamicClient is not initialized")
	}

	// Prepare the patch in the format required by the Kubernetes API
	loggers.LoggerWatcher.Debugf("EG patching labels on HTTPRoute %s/%s: %+v", route.GetNamespace(), route.GetName(), labels)
	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": labels,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %v", err)
	}

	_, err = CRWatcher.DynamicClient.Resource(HTTPRouteGVR).Namespace(route.GetNamespace()).Patch(
		context.Background(),
		route.GetName(),
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to patch HTTPRoute %s/%s: %v", route.GetNamespace(), route.GetName(), err)
	}
	loggers.LoggerWatcher.Infof("Successfully patched labels to HTTPRoute %s/%s", route.GetNamespace(), route.GetName())
	return nil
}

func getLabelValue(route *unstructured.Unstructured, labelKey string) (string, bool) {
	labels := route.GetLabels()
	if labels == nil {
		return "", false
	}
	val, ok := labels[labelKey]
	return val, ok
}

// getRequestsPerUnit extracts the requests-per-unit info from BTP
func getRequestsPerUnit(egBTP *gatewayv1alpha1.BackendTrafficPolicy) (uint, string, bool) {
    if egBTP.Spec.RateLimit != nil {
        if egBTP.Spec.RateLimit.Global != nil && len(egBTP.Spec.RateLimit.Global.Rules) > 0 {
            rule := egBTP.Spec.RateLimit.Global.Rules[0] // pick first rule
            return rule.Limit.Requests, string(rule.Limit.Unit), true
        }
        if egBTP.Spec.RateLimit.Local != nil && len(egBTP.Spec.RateLimit.Local.Rules) > 0 {
            rule := egBTP.Spec.RateLimit.Local.Rules[0]
            return rule.Limit.Requests, string(rule.Limit.Unit), true
        }
    }
    return 0, "", false
}

// getRateLimitIdentifier returns a string like "100requestsperminute"
func getRateLimitIdentifier(egBTP *gatewayv1alpha1.BackendTrafficPolicy) (string, bool) {
    if egBTP.Spec.RateLimit != nil {
        if egBTP.Spec.RateLimit.Global != nil && len(egBTP.Spec.RateLimit.Global.Rules) > 0 {
            rule := egBTP.Spec.RateLimit.Global.Rules[0]
            return fmt.Sprintf("%drequestsper%s", rule.Limit.Requests, strings.ToLower(string(rule.Limit.Unit))), true
        }
        if egBTP.Spec.RateLimit.Local != nil && len(egBTP.Spec.RateLimit.Local.Rules) > 0 {
            rule := egBTP.Spec.RateLimit.Local.Rules[0]
            return fmt.Sprintf("%drequestsper%s", rule.Limit.Requests, strings.ToLower(string(rule.Limit.Unit))), true
        }
    }
    return "", false
}
