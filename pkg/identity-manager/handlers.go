///////////////////////////////////////////////////////////////////////
// Copyright (c) 2017 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
///////////////////////////////////////////////////////////////////////

package identitymanager

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/casbin/casbin"
	jwt "github.com/dgrijalva/jwt-go"
	apiErrors "github.com/go-openapi/errors"
	middleware "github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/vmware/dispatch/pkg/version"

	"github.com/vmware/dispatch/pkg/api/v1"
	"github.com/vmware/dispatch/pkg/controller"
	"github.com/vmware/dispatch/pkg/entity-store"
	"github.com/vmware/dispatch/pkg/identity-manager/gen/restapi/operations"
	orgOperations "github.com/vmware/dispatch/pkg/identity-manager/gen/restapi/operations/organization"
	policyOperations "github.com/vmware/dispatch/pkg/identity-manager/gen/restapi/operations/policy"
	svcAccountOperations "github.com/vmware/dispatch/pkg/identity-manager/gen/restapi/operations/serviceaccount"
	"github.com/vmware/dispatch/pkg/trace"
)

// IdentityManagerFlags are configuration flags for the identity manager
var IdentityManagerFlags = struct {
	CookieName           string `long:"cookie-name" description:"The cookie name used to identify users" default:"_oauth2_proxy"`
	SkipAuth             bool   `long:"skip-auth" description:"Skips authorization, not to be used in production env"`
	BootstrapConfigPath  string `long:"bootstrap-config-path" description:"The path that contains the bootstrap keys" default:"/bootstrap"`
	DbFile               string `long:"db-file" description:"Backend DB URL/Path" default:"./db.bolt"`
	DbBackend            string `long:"db-backend" description:"Backend DB Name" default:"boltdb"`
	DbUser               string `long:"db-username" description:"Backend DB Username" default:"dispatch"`
	DbPassword           string `long:"db-password" description:"Backend DB Password" default:"dispatch"`
	DbDatabase           string `long:"db-database" description:"Backend DB Name" default:"dispatch"`
	ResyncPeriod         int    `long:"resync-period" description:"The time period (in seconds) to refresh policies" default:"30"`
	OAuth2ProxyAuthURL   string `long:"oauth2-proxy-auth-url" description:"The localhost url for oauth2proxy service's auth endpoint'" default:"http://localhost:4180/v1/iam/oauth2/auth"`
	ServiceAccountDomain string `long:"service-account-domain" description:"The default domain name to use for service accounts" default:"svc.dispatch.local"`
	Tracer               string `long:"tracer" description:"Open Tracing Tracer endpoint" default:""`
}{}

const (
	// Policy Model - Use an ACL model that matches request attributes
	// Request Definition - <Requested Org> <Subject> <Resource> <Action>
	// Policy Definition - <Global Policy?> <Subject's Org> <Subject> <Resource> <Action>
	// Matcher - if it's a global policy, allow cross-organization requests otherwise restrict the access to the organization associated with the subject.
	casbinPolicyModel = `
[request_definition]
r = org, sub, res, act
[policy_definition]
p = global, org, sub, res, act
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = (p.global == "y" || r.org == p.org) && r.sub == p.sub && (r.res == p.res || p.res == "*") && (r.act == p.act || p.act == "*")
`
)

// HTTP constants
const (
	HTTPHeaderReqURI     = "X-Auth-Request-Redirect"
	HTTPHeaderOrigMethod = "X-Original-Method"
	HTTPHeaderEmail      = "X-Auth-Request-Email"
)

// Identity manager action constants
const (
	ActionGet    Action = "get"
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// Action defines the type for an action
type Action string

// Identity manager resources type constants
const (
	ResourceIAM Resource = "iam"
)

// Resource defines the type for a resource
type Resource string

// Handlers defines the interface for the identity manager handlers
type Handlers struct {
	watcher  controller.Watcher
	store    entitystore.EntityStore
	enforcer *casbin.SyncedEnforcer
}

// NewHandlers create a new Policy Manager Handler
func NewHandlers(watcher controller.Watcher, store entitystore.EntityStore, enforcer *casbin.SyncedEnforcer) *Handlers {
	return &Handlers{
		watcher:  watcher,
		store:    store,
		enforcer: enforcer,
	}
}

// SetupEnforcer sets up the casbin enforcer
func SetupEnforcer(store entitystore.EntityStore) *casbin.SyncedEnforcer {
	model := casbin.NewModel(casbinPolicyModel)
	adapter := NewCasbinEntityAdapter(store)
	enforcer := casbin.NewSyncedEnforcer(model, adapter)
	return enforcer
}

func (h *Handlers) authenticateCookie(token string) (interface{}, error) {
	// For testing/dev environments only
	if IdentityManagerFlags.SkipAuth {
		log.Warn("Skipping authentication. This is not recommended in production environments.")
		return "", nil
	}
	// Make a request to Oauth2Proxy to validate the cookie. Oauth2Proxy must be setup locally
	proxyReq, err := http.NewRequest(http.MethodGet, IdentityManagerFlags.OAuth2ProxyAuthURL, nil)
	if err != nil {
		msg := "error creating forwarding request to oauth2proxy: %s"
		log.Debugf(msg, err)
		return nil, apiErrors.New(http.StatusUnauthorized, msg, err)
	}

	proxyReq.Header.Set("Cookie", token)
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		msg := "error forwarding request to oauth2proxy: %s"
		log.Debugf(msg, err)
		return nil, apiErrors.New(http.StatusUnauthorized, msg, err)
	}
	if resp.StatusCode != http.StatusAccepted {
		msg := "authentication failed with oauth2proxy: error code %v"
		log.Debugf(msg, resp.StatusCode)
		return nil, apiErrors.New(http.StatusUnauthorized, msg, resp.StatusCode)
	}

	// If authenticated, get subject
	log.Debugf("Received Headers from oauth2proxy %s", resp.Header)
	subject := resp.Header.Get(HTTPHeaderEmail)
	if subject == "" {
		msg := "authentication failed: missing %s header in response from oauth2proxy"
		log.Debugf(msg, HTTPHeaderEmail)
		return nil, apiErrors.New(http.StatusUnauthorized, msg, HTTPHeaderEmail)
	}
	// Valid Cookie return the auth principal
	account := &authAccount{
		organizationID: "",
		subject:        subject,
		kind:           subjectUser,
	}
	return account, nil
}

func (h *Handlers) authenticateBearer(token string) (interface{}, error) {
	// For testing/dev environments only
	if IdentityManagerFlags.SkipAuth {
		log.Warn("Skipping authentication. This is not recommended in production environments.")
		return "", nil
	}

	parts := strings.Split(token, " ")
	if len(parts) < 2 || strings.ToLower(parts[0]) != "bearer" {
		msg := "invalid Authorization header, it must be of form 'Authorization: Bearer <token>'"
		log.Debugf(msg)
		return nil, apiErrors.New(http.StatusUnauthorized, msg)
	}

	jwtToken := parts[1]
	account, err := h.getAuthAccountFromToken(jwtToken)
	if err != nil {
		msg := "unable to validate bearer token: %s"
		log.Debugf(msg, err)
		return nil, apiErrors.New(http.StatusUnauthorized, msg, err)
	}
	return account, nil
}

func (h *Handlers) getAuthAccountFromToken(token string) (*authAccount, error) {

	claims := jwt.MapClaims{}
	new(jwt.Parser).ParseUnverified(token, claims)
	var unverifiedIssuer string
	if s, ok := claims["iss"]; ok {
		unverifiedIssuer = s.(string)
		log.Debugf("identified issuer %s from unvalidated token", unverifiedIssuer)
	} else {
		// Missing issuer claim
		return nil, errors.New("missing issuer claim in unvalidated token")
	}

	var account *authAccount
	var pubBase64Encoded string
	// Get Public Key from secret if bootstrap mode is enabled
	if bootstrapUser := getBootstrapKey("bootstrap_user"); bootstrapUser == unverifiedIssuer {
		log.Warn("Bootstrap mode is enabled. Please ensure it is turned off in a production environment.")
		if bootstrapPubKey := getBootstrapKey("bootstrap_public_key"); bootstrapPubKey != "" {
			pubBase64Encoded = bootstrapPubKey
			account = &authAccount{
				organizationID: "",
				subject:        bootstrapUser,
				kind:           subjectBootstrapUser,
			}
		} else {
			msg := "missing public key in bootstrap mode"
			log.Debugf(msg)
			return nil, errors.New(msg)
		}
	} else {
		// Fetch Public Key from service account record
		svcAccount := ServiceAccount{}
		opts := entitystore.Options{
			Filter: entitystore.FilterExists(),
		}
		log.Debugf("Fetching service account %s from backend", unverifiedIssuer)
		res := strings.Split(unverifiedIssuer, "/")
		if len(res) != 2 {
			return nil, errors.New("invalid issuer claim: missing org info")
		}
		if err := h.store.Get(context.TODO(), res[0], res[1], opts, &svcAccount); err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("store error when getting service account %s", unverifiedIssuer))
		}
		pubBase64Encoded = svcAccount.PublicKey
		account = &authAccount{
			organizationID: svcAccount.OrganizationID,
			subject:        svcAccount.Name,
			kind:           subjectSvcAccount,
		}
	}

	// Decode and validate token with the Public Key
	pubPEM, err := base64.StdEncoding.DecodeString(pubBase64Encoded)
	publicRSAKey, err := jwt.ParseRSAPublicKeyFromPEM(pubPEM)
	if err != nil {
		return nil, errors.Wrap(err, "error while parsing public key")
	}

	// Now, validate the token
	if err := h.validateJWTToken(token, publicRSAKey); err != nil {
		return nil, err
	}

	// Valid token
	return account, nil

}

func (h *Handlers) validateJWTToken(token string, pubKey *rsa.PublicKey) error {

	_, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		// Validate algorithm is same as expected. This is important after the vulnerabilities with JWT using asymmetric
		// keys that don't validate the algorithm.
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		// TODO: Validate Audience claim to ensure the token was issued to this Dispatch Service. Technically speaking
		// the public key must not be re-used for another Dispatch service but it's best to validae this.
		// TODO: Validate Token issued duration was not more than 1 hour (or min duration setting)
		return pubKey, nil
	})
	log.Debugf("Checking valid token")
	if err != nil {
		log.Debugf("Error validating token: %s", err)
		return errors.Wrap(err, "error validating token")
	}
	return nil
}

// ConfigureHandlers registers the identity manager handlers to the API
func (h *Handlers) ConfigureHandlers(api middleware.RoutableAPI) {

	a, ok := api.(*operations.IdentityManagerAPI)
	if !ok {
		panic("Cannot configure api")
	}

	a.CookieAuth = h.authenticateCookie

	a.BearerAuth = h.authenticateBearer

	a.RootHandler = operations.RootHandlerFunc(h.root)
	a.HomeHandler = operations.HomeHandlerFunc(h.home)
	a.AuthHandler = operations.AuthHandlerFunc(h.auth)
	a.RedirectHandler = operations.RedirectHandlerFunc(h.redirect)
	a.GetVersionHandler = operations.GetVersionHandlerFunc(h.getVersion)
	// Policy API Handlers
	a.PolicyAddPolicyHandler = policyOperations.AddPolicyHandlerFunc(h.addPolicy)
	a.PolicyGetPoliciesHandler = policyOperations.GetPoliciesHandlerFunc(h.getPolicies)
	a.PolicyGetPolicyHandler = policyOperations.GetPolicyHandlerFunc(h.getPolicy)
	a.PolicyDeletePolicyHandler = policyOperations.DeletePolicyHandlerFunc(h.deletePolicy)
	a.PolicyUpdatePolicyHandler = policyOperations.UpdatePolicyHandlerFunc(h.updatePolicy)
	// Service Account API Handlers
	a.ServiceaccountAddServiceAccountHandler = svcAccountOperations.AddServiceAccountHandlerFunc(h.addServiceAccount)
	a.ServiceaccountGetServiceAccountHandler = svcAccountOperations.GetServiceAccountHandlerFunc(h.getServiceAccount)
	a.ServiceaccountGetServiceAccountsHandler = svcAccountOperations.GetServiceAccountsHandlerFunc(h.getServiceAccounts)
	a.ServiceaccountDeleteServiceAccountHandler = svcAccountOperations.DeleteServiceAccountHandlerFunc(h.deleteServiceAccount)
	a.ServiceaccountUpdateServiceAccountHandler = svcAccountOperations.UpdateServiceAccountHandlerFunc(h.updateServiceAccount)
	// Organization API Handlers
	a.OrganizationAddOrganizationHandler = orgOperations.AddOrganizationHandlerFunc(h.addOrganization)
	a.OrganizationGetOrganizationHandler = orgOperations.GetOrganizationHandlerFunc(h.getOrganization)
	a.OrganizationGetOrganizationsHandler = orgOperations.GetOrganizationsHandlerFunc(h.getOrganizations)
	a.OrganizationDeleteOrganizationHandler = orgOperations.DeleteOrganizationHandlerFunc(h.deleteOrganization)
	a.OrganizationUpdateOrganizationHandler = orgOperations.UpdateOrganizationHandlerFunc(h.updateOrganization)
}

func (h *Handlers) root(params operations.RootParams) middleware.Responder {
	message := "Default Root Page"
	return operations.NewRootOK().WithPayload(
		&v1.Message{Message: swag.String(message)})
}

func (h *Handlers) home(params operations.HomeParams, principal interface{}) middleware.Responder {

	message := "Home Page, You have already logged in"
	return operations.NewHomeOK().WithPayload(
		&v1.Message{Message: swag.String(message)})
}

func (h *Handlers) auth(params operations.AuthParams, principal interface{}) middleware.Responder {
	span, ctx := trace.Trace(params.HTTPRequest.Context(), "")
	defer span.Finish()

	// For development use cases, not recommended in production env.
	if IdentityManagerFlags.SkipAuth {
		log.Warn("Skipping authorization. This is not recommended in production environments.")
		if params.XDispatchOrg == nil {
			return operations.NewAuthAccepted().WithXDispatchOrg("")
		}
		return operations.NewAuthAccepted().WithXDispatchOrg(*params.XDispatchOrg)
	}

	// At this point, the principal is authenticated, let's do a policy check.
	account := principal.(*authAccount)

	// Verify request
	reqAttrs, err := getRequestAttributes(params.HTTPRequest, account.subject)
	if err != nil {
		log.Debugf("Invalid request, unable to parse request attributes: %s", err)
		return operations.NewAuthForbidden()
	}

	// Skip policy check for bootstrap user
	if account.kind == subjectBootstrapUser {
		if reqAttrs.isResourceRequest && Resource(reqAttrs.resource) != ResourceIAM {
			log.Warn("Cannot operate on a non-iam resource during bootstrap, auth forbidden")
			return operations.NewAuthForbidden()
		}
		log.Info("Bootstrap auth accepted")
		var bootstrapOrg string
		if params.XDispatchOrg != nil {
			bootstrapOrg = *params.XDispatchOrg
		}
		return operations.NewAuthAccepted().WithXDispatchOrg(bootstrapOrg)
	}

	// For User accounts, orgID can be missing after authentication, it just means the upstream IDP is not multi-tenant or
	// Dispatch isn't configured with the claims that identify tenancy. Proceed with checking policies against user-
	// specified org-id in XDispatchOrg Header.
	if account.kind == subjectUser && account.organizationID == "" {
		if params.XDispatchOrg == nil {
			log.Debug("Missing X-DISPATCH-ORG Header")
			return operations.NewAuthForbidden()
		}
		account.organizationID = *params.XDispatchOrg
	}

	var requestedOrg string
	// If X-Dispatch-Org Header is missing, use the subject's org for policy enforcement
	if params.XDispatchOrg != nil {
		requestedOrg = *params.XDispatchOrg
	} else {
		requestedOrg = account.organizationID
	}

	// Validate Organization specified in request
	if !checkOrgExists(ctx, h.store, requestedOrg) {
		return operations.NewAuthForbidden()
	}

	// Skip policy check for non-resource requests
	if !reqAttrs.isResourceRequest {
		return operations.NewAuthAccepted().WithXDispatchOrg(requestedOrg)
	}

	log.Debugf("Enforcing Policy: %s, %s, %s, %s\n", requestedOrg, reqAttrs.subject, reqAttrs.resource, reqAttrs.action)
	if h.enforcer.Enforce(requestedOrg, reqAttrs.subject, reqAttrs.resource, string(reqAttrs.action)) == true {
		// TODO: Return the org-id associated with this user.
		return operations.NewAuthAccepted().WithXDispatchOrg(requestedOrg)
	}

	// deny the request, show an error
	return operations.NewAuthForbidden()
}

func (h *Handlers) redirect(params operations.RedirectParams, principal interface{}) middleware.Responder {

	redirect := *params.Redirect
	cookie, err := params.HTTPRequest.Cookie(IdentityManagerFlags.CookieName)
	if err != nil {
		return operations.NewRedirectDefault(http.StatusInternalServerError).WithPayload(
			&v1.Error{Code: http.StatusInternalServerError,
				Message: swag.String("No Such Cookie")})
	}

	values := url.Values{
		"cookie": {cookie.String()},
	}
	location := fmt.Sprintf("%s?%s", redirect, values.Encode())
	return operations.NewRedirectFound().WithLocation(location)
}

func (h *Handlers) getVersion(params operations.GetVersionParams) middleware.Responder {
	return operations.NewGetVersionOK().WithPayload(version.Get())
}

func checkOrgExists(ctx context.Context, store entitystore.EntityStore, orgName string) bool {
	opts := entitystore.Options{
		Filter: entitystore.FilterExists(),
	}
	org := Organization{}
	if err := store.Get(ctx, orgName, orgName, opts, &org); err != nil {
		log.Errorf("store error when getting organization '%s': %s", orgName, err)
		return false
	}
	return true
}

func getRequestAttributes(request *http.Request, subject string) (*attributesRecord, error) {
	log.Debugf("Headers: %s; Subject %s\n", request.Header, subject)

	if strings.TrimSpace(subject) == "" {
		return nil, fmt.Errorf("subject cannot be empty")
	}
	// Map REST verb from http.Request to policy actions
	requestMethod := request.Header.Get(HTTPHeaderOrigMethod)
	if requestMethod == "" {
		return nil, fmt.Errorf("%s header not found", HTTPHeaderOrigMethod)
	}
	var action Action
	switch requestMethod {
	case http.MethodGet:
		action = ActionGet
	case http.MethodPost:
		action = ActionCreate
	case http.MethodPut:
		action = ActionUpdate
	case http.MethodPatch:
		action = ActionUpdate
	case http.MethodDelete:
		action = ActionDelete
	}

	// Determine resource/non-resource paths from the Original URL
	// Valid resource paths are:
	// /{version}/{resource}
	// /{version}/{resource}/{resourceName|resourceID}
	//
	// Valid non-resource paths:
	// /
	// /{version}
	// /{specialPrefix} e.g /echo
	requestPath := request.Header.Get(HTTPHeaderReqURI)
	log.Debugf("Request path: %s", requestPath)
	if requestPath == "" {
		return nil, fmt.Errorf("%s header not found", HTTPHeaderReqURI)
	}
	currentParts := strings.Split(strings.Trim(requestPath, "/"), "/")
	// Check if a nonResource path is requested
	if len(currentParts) < 2 {
		return &attributesRecord{
			subject:           subject,
			path:              requestPath,
			isResourceRequest: false,
			action:            action,
		}, nil
	}
	// Note: skipping version information in parts[0]. This can be used in the future to narrow down the request scope.
	return &attributesRecord{
		subject:           subject,
		isResourceRequest: true,
		resource:          currentParts[1],
		action:            action,
	}, nil
}

func getBootstrapKey(key string) string {
	bootstrapUserFile := filepath.Join(IdentityManagerFlags.BootstrapConfigPath, key)
	value, err := ioutil.ReadFile(bootstrapUserFile)
	if err != nil {
		log.Debugf("unable to read bootstrap key %s file: %s", bootstrapUserFile, err)
		return ""
	}
	return string(value)
}
