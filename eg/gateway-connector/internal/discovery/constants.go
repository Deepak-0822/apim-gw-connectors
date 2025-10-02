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

import "k8s.io/apimachinery/pkg/runtime/schema"

// GVRs to watch in EG discovery
var (
	HTTPRouteGVR      = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	SecurityPolicyGVR = schema.GroupVersionResource{
		Group:    "gateway.envoyproxy.io",
		Version:  "v1alpha1",
		Resource: "securitypolicies",
	}
	BackendTrafficPolicyGVR = schema.GroupVersionResource{
		Group:    "gateway.envoyproxy.io",
		Version:  "v1alpha1",          
		Resource: "backendtrafficpolicies", 
	}
	ConfigMapGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
)

// Core kinds/fields/labels
const (
	HTTPRouteKind  = "HTTPRoute"
	SecurityPolicyKind = "SecurityPolicy"
	BackendTrafficPolicyKind = "BackendTrafficPolicy"
	ServiceKind    = "Service"
	SpecField      = "spec"
	RulesField     = "rules"
	MatchesField   = "matches"
	HostnamesField = "hostnames"
	PathField      = "path"
	ValueField     = "value"
	MethodField    = "method"

	MetadataField  = "metadata"
	LabelsField    = "labels"
	NameField      = "name"
	NamespaceField = "namespace"
	DataField      = "data"
)

// Labels and annotations
const (
	InitiatedFromLabel = "InitiateFrom"
	ControlPlaneOrigin = "CP"
	DataPlaneOrigin    = "DP"
)

// Environment variable keys and defaults for label/selectors
const (
	EnvLabelAPINameKey     = "EG_API_NAME_LABEL"
	EnvLabelAPISpecKey     = "EG_OPENAPI_SPEC_CONFIGMAP_LABEL"
	EnvConfigMapKeyNameKey = "EG_OPENAPI_SPEC_CONFIGMAP_KEY"

	DefaultAPINameLabel     = "wso2-api-name"
	DefaultAPISpecLabel     = "wso2-open-api-spec"
	DefaultConfigMapKeyName = "openapispec"
)

// Misc
const (
	EmptyString       = ""
	DefaultHTTPMethod = "GET"
	PathSeparator     = "/"
)
