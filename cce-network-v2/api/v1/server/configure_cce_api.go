// This file is safe to edit. Once it exists it will not be overwritten

// Copyright Authors of Baidu AI Cloud
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/daemon"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
)

//go:generate swagger generate server --target ../../v1 --name CceAPI --spec ../openapi.yaml --api-package restapi --server-package server --principal interface{} --default-scheme unix

func configureFlags(api *restapi.CceAPIAPI) {
	// api.CommandLineOptionsGroups = []swag.CommandLineOptionsGroup{ ... }
}

func configureAPI(api *restapi.CceAPIAPI) http.Handler {
	// configure the api here
	api.ServeError = errors.ServeError

	// Set your custom logger if needed. Default one is log.Printf
	// Expected interface func(string, ...interface{})
	//
	// Example:
	// api.Logger = log.Printf

	// api.UseSwaggerUI()
	// To continue using redoc as your UI, uncomment the following line
	// api.UseRedoc()

	api.JSONConsumer = runtime.JSONConsumer()

	api.JSONProducer = runtime.JSONProducer()

	if api.IpamDeleteIpamIPHandler == nil {
		api.IpamDeleteIpamIPHandler = ipam.DeleteIpamIPHandlerFunc(func(params ipam.DeleteIpamIPParams) middleware.Responder {
			return middleware.NotImplemented("operation ipam.DeleteIpamIP has not yet been implemented")
		})
	}
	if api.DaemonGetHealthzHandler == nil {
		api.DaemonGetHealthzHandler = daemon.GetHealthzHandlerFunc(func(params daemon.GetHealthzParams) middleware.Responder {
			return middleware.NotImplemented("operation daemon.GetHealthz has not yet been implemented")
		})
	}
	if api.EniPostEniHandler == nil {
		api.EniPostEniHandler = eni.PostEniHandlerFunc(func(params eni.PostEniParams) middleware.Responder {
			return middleware.NotImplemented("operation eni.PostEni has not yet been implemented")
		})
	}
	if api.IpamPostIpamHandler == nil {
		api.IpamPostIpamHandler = ipam.PostIpamHandlerFunc(func(params ipam.PostIpamParams) middleware.Responder {
			return middleware.NotImplemented("operation ipam.PostIpam has not yet been implemented")
		})
	}
	if api.IpamPostIpamIPHandler == nil {
		api.IpamPostIpamIPHandler = ipam.PostIpamIPHandlerFunc(func(params ipam.PostIpamIPParams) middleware.Responder {
			return middleware.NotImplemented("operation ipam.PostIpamIP has not yet been implemented")
		})
	}

	api.PreServerShutdown = func() {}

	api.ServerShutdown = func() {
		logging.DefaultLogger.Debug("canceling server context")
		serverCancel()
	}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

// The TLS configuration before HTTPS server starts.
func configureTLS(tlsConfig *tls.Config) {
	// Make all necessary changes to the TLS configuration here.
}

var (
	// ServerCtx and ServerCancel
	ServerCtx, serverCancel = context.WithCancel(context.Background())
)

// As soon as server is initialized but not run yet, this function will be called.
// If you need to modify a config, store server instance to stop it individually later, this is the place.
// This function can be called multiple times, depending on the number of serving schemes.
// scheme value will be set accordingly: "http", "https" or "unix"
func configureServer(s *http.Server, scheme, addr string) {
	s.BaseContext = func(_ net.Listener) context.Context {
		return ServerCtx
	}
}

// The middleware configuration is for the handler executors. These do not apply to the swagger.json document.
// The middleware executes after routing but before authentication, binding and validation
func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

// The middleware configuration happens before anything, this middleware also applies to serving the swagger.json document.
// So this is a good place to plug in a panic handling middleware, logging and metrics
func setupGlobalMiddleware(handler http.Handler) http.Handler {
	eventsHelper := &metrics.APIEventTSHelper{
		Next:      handler,
		TSGauge:   metrics.EventTS,
		Histogram: metrics.APIInteractions,
	}

	return &api.APIPanicHandler{
		Next: eventsHelper,
	}
}
