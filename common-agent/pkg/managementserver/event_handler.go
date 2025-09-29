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
package managementserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wso2-extensions/apim-gw-connectors/common-agent/config"
	logger "github.com/wso2-extensions/apim-gw-connectors/common-agent/pkg/loggers"
	utils "github.com/wso2-extensions/apim-gw-connectors/common-agent/pkg/utils"
)

// HandleDeleteEvent processes a delete event and returns an error if it fails
func HandleDeleteEvent(event APICPEvent) (bool, error) {
	cpConfig, err := config.ReadConfigs()
	envLabel := []string{"Default"}
	if err == nil {
		envLabel = cpConfig.ControlPlane.EnvironmentLabels
	}

	logger.LoggerMgtServer.Infof("Delete event received with APIUUID: %s", event.API.APIUUID)

	// Fetch all revisions of the API
	revisionsResp, retryableList, err := utils.ListAPIRevisions(event.API.APIUUID, "")
	if err != nil {
		logger.LoggerMgtServer.Errorf("Error while listing revisions for API ID: %s. Error: %+v", event.API.APIUUID, err)
		return retryableList, fmt.Errorf("failed to list revisions: %v", err)
	}

	if revisionsResp == nil || revisionsResp.Count == 0 {
		logger.LoggerMgtServer.Infof("No revisions found for API ID: %s", event.API.APIUUID)
		return false, nil
	}

	var firstError error
	retryable := false || retryableList
	for _, rev := range revisionsResp.List {
		// Try to get revision ID from common keys
		var revID string
		if v, ok := rev["id"].(string); ok && v != "" {
			revID = v
		} else if v, ok := rev["revisionId"].(string); ok && v != "" {
			revID = v
		} else if v, ok := rev["revisionUuid"].(string); ok && v != "" {
			revID = v
		} else if v, ok := rev["uuid"].(string); ok && v != "" {
			revID = v
		}

		if revID == "" {
			logger.LoggerMgtServer.Warnf("Skipping a revision entry without identifiable ID. Entry: %+v", rev)
			continue
		}

		// Build payload per revision
		payload := []map[string]interface{}{
			{
				"revisionUuid":       revID,
				"name":               envLabel[0],
				"vhost":              event.API.Vhost,
				"displayOnDevportal": true,
			},
		}
		jsonPayload, mErr := json.Marshal(payload)
		if mErr != nil {
			logger.LoggerMgtServer.Errorf("Error while preparing payload to delete revision %s. Error: %+v", revID, mErr)
			if firstError == nil {
				firstError = fmt.Errorf("failed to marshal payload for revision %s: %v", revID, mErr)
			}
			continue
		}

		// Delete the API revision (undeploy + delete)
		retryableL, dErr := utils.DeleteAPIRevision(event.API.APIUUID, revID, string(jsonPayload))
		if dErr != nil {
			retryable = retryable || retryableL
			logger.LoggerMgtServer.Errorf("Error while undeploying/deleting api revision. RevisionId: %s, API ID: %s, Error: %+v", revID, event.API.APIUUID, dErr)
			if firstError == nil {
				firstError = fmt.Errorf("failed to delete revision %s: %v", revID, dErr)
			}
			// Continue attempting to delete remaining revisions
			continue
		}
	}

	if firstError != nil {
		return retryable, firstError
	}
	return false, nil
}

// HandleCreateOrUpdateEvent processes create or update events and returns id, revisionID, and error
func HandleCreateOrUpdateEvent(event APICPEvent) (string, string, bool, error) {
	// Set default OpenAPI definition for REST APIs if missing
	if strings.EqualFold(event.API.APIType, "rest") && event.API.Definition == "" {
		event.API.Definition = utils.OpenAPIDefaultYaml
	}
	// Convert JSON to YAML for REST APIs
	if strings.EqualFold(event.API.APIType, "rest") {
		if yaml, err := JSONToYAML(event.API.Definition); err == nil {
			event.API.Definition = yaml
		}
	}

	// Generate API and deployment YAMLs using the injected API YAML creator
	if apiYamlCreator == nil {
		logger.LoggerMgtServer.Errorf("API YAML creator not set.")
		return "", "", false, fmt.Errorf("API YAML creator not configured")
	}
	apiYaml, definition, endpointsYaml := apiYamlCreator.CreateAPIYaml(&event)
	deploymentContent := CreateDeploymentYaml(event.API.Vhost)
	logger.LoggerMgtServer.Debugf("Created apiYaml: %s, \n\n\n created definition file: %s", apiYaml, definition)

	// Determine definition file path
	definitionPath := fmt.Sprintf("%s-%s/Definitions/swagger.yaml", event.API.APIName, event.API.APIVersion)
	if strings.ToUpper(event.API.APIType) == "GRAPHQL" {
		definitionPath = fmt.Sprintf("%s-%s/Definitions/schema.graphql", event.API.APIName, event.API.APIVersion)
	}

	// Prepare zip files
	var zipFiles []utils.ZipFile
	logger.LoggerMgtServer.Debugf("endpoints yaml: %s", endpointsYaml)
	if endpointsYaml != "{}\n" {
		logger.LoggerMgtServer.Debugf("Creating zip file with endpoints")
		zipFiles = []utils.ZipFile{{
			Path:    fmt.Sprintf("%s-%s/api.yaml", event.API.APIName, event.API.APIVersion),
			Content: apiYaml,
		}, {
			Path:    fmt.Sprintf("%s-%s/endpoints.yaml", event.API.APIName, event.API.APIVersion),
			Content: endpointsYaml,
		}, {
			Path:    fmt.Sprintf("%s-%s/deployment_environments.yaml", event.API.APIName, event.API.APIVersion),
			Content: deploymentContent,
		}, {
			Path:    definitionPath,
			Content: definition,
		}}
	} else {
		logger.LoggerMgtServer.Debugf("Creating zip file without endpoints")
		zipFiles = []utils.ZipFile{{
			Path:    fmt.Sprintf("%s-%s/api.yaml", event.API.APIName, event.API.APIVersion),
			Content: apiYaml,
		}, {
			Path:    fmt.Sprintf("%s-%s/deployment_environments.yaml", event.API.APIName, event.API.APIVersion),
			Content: deploymentContent,
		}, {
			Path:    definitionPath,
			Content: definition,
		}}
	}

	var buf bytes.Buffer
	if err := utils.CreateZipFile(&buf, zipFiles); err != nil {
		logger.LoggerMgtServer.Errorf("Error while creating apim zip file for api uuid: %s. Error: %+v", event.API.APIUUID, err)
		return "", "", false, fmt.Errorf("failed to create zip file: %v", err)
	}

	// Import API
	id, revisionID, retryable, err := utils.ImportAPI(fmt.Sprintf("admin-%s-%s.zip", event.API.APIName, event.API.APIVersion), &buf)
	_ = retryable // currently not used in this path
	if err != nil {
		logger.LoggerMgtServer.Errorf("Error while importing API.")
		return "", "", retryable, fmt.Errorf("failed to import API: %v", err)
	}
	return id, revisionID, false, nil
}
