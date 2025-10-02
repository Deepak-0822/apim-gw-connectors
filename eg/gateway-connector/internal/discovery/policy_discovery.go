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
	"strings"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/loggers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	gwapiv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// handleAddOrUpdateSecurityPolicy reacts to SecurityPolicy changes that target HTTPRoutes
func handleAddOrUpdateSecurityPolicy(u *unstructured.Unstructured) {
	loggers.LoggerWatcher.Debugf("EG Processing SecurityPolicy: %s/%s", u.GetNamespace(), u.GetName())
	if !isPolicyAccepted(u) {
		loggers.LoggerWatcher.Debugf("EG SecurityPolicy %s/%s not accepted yet; skipping", u.GetNamespace(), u.GetName())
		return
	}
	reconcileTargetHTTPRoutes(u)
}

// handleAddOrUpdateBackendTrafficPolicy reacts to BackendTrafficPolicy changes that target HTTPRoutes
func handleAddOrUpdateBackendTrafficPolicy(u *unstructured.Unstructured) {
	loggers.LoggerWatcher.Debugf("EG Processing BackendTrafficPolicy: %s/%s", u.GetNamespace(), u.GetName())
	if !isPolicyAccepted(u) {
		loggers.LoggerWatcher.Debugf("EG BackendTrafficPolicy %s/%s not accepted yet; skipping", u.GetNamespace(), u.GetName())
		return
	}
	reconcileTargetHTTPRoutes(u)
}

// handleDeleteSecurityPolicy reacts to SecurityPolicy deletions that target HTTPRoutes
func handleDeleteSecurityPolicy(u *unstructured.Unstructured) {
	loggers.LoggerWatcher.Debugf("EG Processing deleted SecurityPolicy: %s/%s", u.GetNamespace(), u.GetName())
	reconcileTargetHTTPRoutes(u)
}

// handleDeleteBackendTrafficPolicy reacts to BackendTrafficPolicy deletions that target HTTPRoutes
func handleDeleteBackendTrafficPolicy(u *unstructured.Unstructured) {
	loggers.LoggerWatcher.Debugf("EG Processing deleted BackendTrafficPolicy: %s/%s", u.GetNamespace(), u.GetName())
	reconcileTargetHTTPRoutes(u)
}

// isPolicyAccepted checks .status.conditions for any condition with status == True.
func isPolicyAccepted(u *unstructured.Unstructured) bool {
	if u == nil {
		return false
	}
	conditionsPopulated := false
	switch u.GetKind() {
	case "SecurityPolicy":
		var sp egv1a1.SecurityPolicy
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &sp); err != nil {
			return false
		}
		for _, anc := range sp.Status.Ancestors {
			for _, cond := range anc.Conditions {
				conditionsPopulated = true
				if cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != sp.Generation {
					return false
				}
			}
		}

	case "BackendTrafficPolicy":
		var bp egv1a1.BackendTrafficPolicy
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &bp); err != nil {
			return false
		}
		for _, anc := range bp.Status.Ancestors {
			for _, cond := range anc.Conditions {
				conditionsPopulated = true
				if cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != bp.Generation {
					return false
				}
			}
		}
	}

	return conditionsPopulated
}

func reconcileTargetHTTPRoutes(policy *unstructured.Unstructured) {
	if CRWatcher == nil || CRWatcher.DynamicClient == nil {
		loggers.LoggerWatcher.Warn("EG CRWatcher or DynamicClient not initialized; cannot reconcile target HTTPRoutes")
		return
	}

	var refs []gwapiv1a2.LocalPolicyTargetReferenceWithSectionName
	switch policy.GetKind() {
	case "SecurityPolicy":
		var sp egv1a1.SecurityPolicy
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(policy.Object, &sp); err != nil {
			loggers.LoggerWatcher.Errorf("EG failed to convert SecurityPolicy %s/%s: %v", policy.GetNamespace(), policy.GetName(), err)
			return
		}
		if sp.Spec.PolicyTargetReferences.TargetRef != nil {
			// Single targetRef case
			refs = []gwapiv1a2.LocalPolicyTargetReferenceWithSectionName{*sp.Spec.PolicyTargetReferences.TargetRef}
		} else {
			refs = sp.Spec.PolicyTargetReferences.TargetRefs
		}
	case "BackendTrafficPolicy":
		var bp egv1a1.BackendTrafficPolicy
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(policy.Object, &bp); err != nil {
			loggers.LoggerWatcher.Errorf("EG failed to convert BackendTrafficPolicy %s/%s: %v", policy.GetNamespace(), policy.GetName(), err)
			return
		}
		if bp.Spec.PolicyTargetReferences.TargetRef != nil {
			// Single targetRef case
			refs = []gwapiv1a2.LocalPolicyTargetReferenceWithSectionName{*bp.Spec.PolicyTargetReferences.TargetRef}
		} else {
			refs = bp.Spec.PolicyTargetReferences.TargetRefs
		}
	default:
		loggers.LoggerWatcher.Debugf("EG Policy %s/%s has unsupported kind %s", policy.GetNamespace(), policy.GetName(), policy.GetKind())
		return
	}

	if len(refs) == 0 {
		loggers.LoggerWatcher.Debugf("EG Policy %s/%s has no targetRefs; nothing to reconcile", policy.GetNamespace(), policy.GetName())
		return
	}

	defaultNS := policy.GetNamespace()

	for _, ref := range refs {
		if strings.EqualFold(string(ref.Kind), HTTPRouteKind) && (ref.Group == "" || string(ref.Group) == HTTPRouteGVR.Group) {
			ns := defaultNS

			hr, err := CRWatcher.DynamicClient.Resource(HTTPRouteGVR).Namespace(ns).Get(
				context.Background(), string(ref.Name), metav1.GetOptions{})
			if err != nil {
				loggers.LoggerWatcher.Errorf(
					"EG failed to fetch HTTPRoute %s/%s targeted by policy %s/%s: %v",
					ns, ref.Name, policy.GetNamespace(), policy.GetName(), err,
				)
				continue
			}

			if !IsControlPlaneInitiated(hr) {
				handleAddOrUpdateHTTPRoute(nil, hr)
			}
		}
	}
}