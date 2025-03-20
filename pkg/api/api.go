package api

import (
	"net/http"

	"github.com/sirupsen/logrus"
)

// API is the http server responsible for serving the HTTP API endpoints.
type API struct {
	Token       string
	hasHandlers bool
}

// New is a factory function creating a new API instance.
func New(token string) *API {
	return &API{
		Token:       token,
		hasHandlers: false,
	}
}

// RequireToken is wrapper around http.HandleFunc that checks token validity.
func (api *API) RequireToken(handleFunc http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		want := "Bearer " + api.Token

		if auth != want {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		logrus.Debug("Valid token found.")
		handleFunc(w, r)
	}
}

// RegisterFunc is a wrapper around http.HandleFunc that also sets the flag used to determine whether to launch the API.
func (api *API) RegisterFunc(path string, fn http.HandlerFunc) {
	api.hasHandlers = true
	http.HandleFunc(path, api.RequireToken(fn))
}

// RegisterHandler is a wrapper around http.Handler that also sets the flag used to determine whether to launch the API.
func (api *API) RegisterHandler(path string, handler http.Handler) {
	api.hasHandlers = true
	http.Handle(path, api.RequireToken(handler.ServeHTTP))
}

// Start the API and serve over HTTP. Requires an API Token to be set.
func (api *API) Start(block bool) error {
	if !api.hasHandlers {
		logrus.Debug("Watchtower HTTP API skipped.")

		return nil
	}

	if api.Token == "" {
		logrus.Fatal("api token is empty or has not been set. exiting")
	}

	if block {
		runHTTPServer()
	} else {
		go func() {
			runHTTPServer()
		}()
	}

	return nil
}

func runHTTPServer() {
	logrus.Fatal(http.ListenAndServe(":8080", nil))
}
