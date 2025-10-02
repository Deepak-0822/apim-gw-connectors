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

package constants

import "os"

// Gateway related constants
const (
	GatewayName  = "wso2-apk-default"
	GatewayGroup = "gateway.networking.k8s.io"
	GatewayKind  = "Gateway"
)

// TokenIssuer related constants
const (
	ConsumerKeyClaim           = "azp"
	ScopesClaim                = "scope"
	InternalKeyTokenIssuerName = "Internal Key TokenIssuer"
	InternalKeySecretName      = "apim-common-agent-issuer-cert"
	InternalKeySecretKey       = "wso2.crt"
	InternalKeySuffix          = "-internal-key-issuer"
)

const (
	LabelAPIID      = "apiID"
	LabelRevisionID = "revisionID"
	LabelAPIUUID    = "apiUUID"
	LabelAPIHash    = "apiHash"
)

const (
	EnvInProgressExpiryKey               = "APIM_INPROGRESS_EXPIRY"
	DefaultInProgressExpiry              = "100"
	EnvWaitTimeAfterInitialDiscoveryKey  = "APIM_WAIT_TIME_AFTER_INITIAL_DISCOVERY"
	DefaultWaitTimeAfterInitialDiscovery = "1000"
)

// Agent name handling
// APIM_AGENT_NAME can be used to override the default agent name (defaults to "EG").
// Keep a single place to derive this so all emits use the same value.
const (
	envAgentNameKey  = "APIM_AGENT_NAME"
	defaultAgentName = "EG"
)

// GetAgentName returns the agent name from environment, falling back to defaultAgentName.
func GetAgentName() string {
	if v := os.Getenv(envAgentNameKey); v != "" {
		return v
	}
	return defaultAgentName
}
