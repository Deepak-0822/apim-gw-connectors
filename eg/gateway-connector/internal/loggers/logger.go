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

// Package loggers contains the package references for log messages
// If a new package is introduced, the corresponding logger reference is need to be created as well.
package loggers

import (
	"github.com/sirupsen/logrus"
	"github.com/wso2-extensions/apim-gw-connectors/common-agent/pkg/logging"
)

/* loggers should be initiated only for the main packages
 ********** Don't initiate loggers for sub packages ****************

When you add a new logger instance add the related package name as a constant
*/

// package name constants
const (
	pkgAgent        = "github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/agent"
	pkgK8sClient    = "github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/k8sclient"
	pkgMapper       = "github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/mapper"
	pkgMessaging    = "github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/messaging"
	pkgSynchronizer = "github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/synchronizer"
	pkgUtils        = "github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/utils"
	pkgEventhub     = "github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/eventhub"
	pkgWatcher      = "github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/discovery"
)

// logger package references
var (
	LoggerK8sClient    logging.Log
	LoggerMapper       logging.Log
	LoggerMessaging    logging.Log
	LoggerSynchronizer logging.Log
	LoggerUtils        logging.Log
	LoggerAgent        logging.Log
	LoggerEventhub     logging.Log
	LoggerWatcher      logging.Log
)

func init() {
	UpdateLoggers()
}

// UpdateLoggers initializes the logger package references
func UpdateLoggers() {
	LoggerK8sClient = logging.InitPackageLogger(pkgK8sClient)
	LoggerMapper = logging.InitPackageLogger(pkgMapper)
	LoggerMessaging = logging.InitPackageLogger(pkgMessaging)
	LoggerSynchronizer = logging.InitPackageLogger(pkgSynchronizer)
	LoggerUtils = logging.InitPackageLogger(pkgUtils)
	LoggerAgent = logging.InitPackageLogger(pkgAgent)
	LoggerEventhub = logging.InitPackageLogger(pkgEventhub)
	LoggerWatcher = logging.InitPackageLogger(pkgWatcher)
	logrus.Info("Updated apk agent loggers")
}
