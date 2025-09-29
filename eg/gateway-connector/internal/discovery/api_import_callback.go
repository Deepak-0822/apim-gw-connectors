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

	"github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/loggers"
	"github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/constants"
)

// EnvoyAPIImportCallback implements the APIImportCallback interface for Envoy gateway connector
type EnvoyAPIImportCallback struct{}

// OnAPIImportSuccess is called when an API has been successfully imported to the control plane.
// It updates the Service and HTTPRoute CRs with the apiID label for Envoy discovery mode.
func (k *EnvoyAPIImportCallback) OnAPIImportSuccess(envoyAPIUUID, apiID, revisionID, crName, crNamespace, agentName string) {
	loggers.LoggerWatcher.Infof("%s API import callback triggered - envoyAPIUUID: %s, apiID: %s, revisionID: %s, CR: %s/%s",
		agentName, envoyAPIUUID, apiID, revisionID, crNamespace, crName)
	// Get all related HTTPRoutes
	hrs, err := getAllRelatedHttpRoutes(crName, crNamespace)
	if err != nil {
		loggers.LoggerWatcher.Errorf("Failed to fetch HTTPRoutes for API %s: %v", envoyAPIUUID, err)
		return
	}
	hash := computeAPIHash(hrs)
	for _, hr := range hrs {
		if err := patchLabelsToHTTPRoute(hr, map[string]string{
			constants.LabelAPIID: apiID,
			constants.LabelRevisionID: revisionID,
			constants.LabelAPIUUID: envoyAPIUUID,
			constants.LabelAPIHash: hash,
		}); err != nil {
			loggers.LoggerWatcher.Errorf("Failed to patch labels to HTTPRoute %s/%s for API %s: %v", hr.GetNamespace(), hr.GetName(), envoyAPIUUID, err)
		} else {
			loggers.LoggerWatcher.Infof("Successfully patched labels to HTTPRoute %s/%s for API %s", hr.GetNamespace(), hr.GetName(), envoyAPIUUID)
		}
	}
}