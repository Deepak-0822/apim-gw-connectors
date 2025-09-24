/*
 * Copyright (c) 2025 WSO2 LLC. (http://www.wso2.org) All Rights Reserved.
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package org.wso2.kong.client;

/**
 * This class contains the constants used in Kong client.
 */
public class KongConstants {
    public static final String KONG_TYPE = "Kong";
    public static final String KONG_ADMIN_URL = "admin_url";
    public static final String KONG_CONTROL_PLANE_ID = "control_plane_id";
    public static final String KONG_AUTH_TOKEN = "auth_key";

    public static final String KONG_DEPLOYMENT_TYPE = "deployment_type";
    public static final String KONG_STANDALONE_DEPLOYMENT = "Standalone";
    public static final String KONG_KUBERNETES_DEPLOYMENT = "Kubernetes";

    // Kong Plugin Types
    public static final String KONG_CORS_PLUGIN_TYPE = "cors";
    public static final String KONG_RATELIMIT_ADVANCED_PLUGIN_TYPE = "rate-limiting-advanced";
    public static final String KONG_RATELIMIT_PLUGIN_TYPE = "rate-limiting";

    // Commonly used default values and headers
    public static final String AUTHORIZATION_HEADER = "Authorization";
    public static final String BEARER_PREFIX = "Bearer ";
    public static final int DEFAULT_API_LIST_LIMIT = 1000;
    public static final int DEFAULT_PLUGIN_LIST_LIMIT = 100;
    public static final int DEFAULT_SERVICE_LIST_LIMIT = 1000;
    public static final int DEFAULT_ROUTE_LIST_LIMIT = 1000;
    public static final String DEFAULT_API_PROVIDER = "admin";
    public static final String DEFAULT_API_VERSION = "v1";
    public static final String DEFAULT_TIER = "Unlimited";
    public static final String DEFAULT_GATEWAY_VENDOR = "external";
    public static final String DEFAULT_VHOST = "example.com";
}
