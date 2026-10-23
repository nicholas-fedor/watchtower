// Package routes registers the enabled HTTP API endpoints on the Fiber
// application.
//
// Each endpoint uses the handler from its handlers subpackage, wrapped in the
// middleware that endpoint requires, such as authentication and a request
// timeout. The package also defines the options that configure the API
// server, which the api package passes in.
package routes
