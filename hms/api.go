package hms

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
)

type APIKey struct {
	APIKey     string
	OwnerEmail string
	Created    time.Time
}

type appError struct {
	Error   error
	Message string
	Code    int
}

type AddSuccessResponse struct {
	Success   bool
	ResultURL string
}

type ResolveResponse struct {
	Success bool
	Result  Link
}

type appHandler func(http.ResponseWriter, *http.Request) *appError
type apiHandler func(http.ResponseWriter, *http.Request, APIKey) *appError

var apiRoutes = map[string]apiHandler{
	"/api/add":     handleAdd,
	"/api/resolve": handleResolve,
}

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if e := fn(w, r); e != nil {
		c := appengine.NewContext(r)
		if e.Code == 500 {
			log.Errorf(c, "error recorded: %v; message: %v", e.Error, e.Message)
			http.Error(w, e.Message, e.Code)
		} else {
			asJson, _ := json.Marshal(e)
			http.Error(w, string(asJson), e.Code)
		}
	}
}

func makeAPIKey(c context.Context) *datastore.Key {
	return datastore.NewKey(c, "APIKey", "default_apikey", 0, nil)
}

func handleAdd(w http.ResponseWriter, r *http.Request, apiKey APIKey) *appError {
	if r.Method != "POST" {
		return &appError{nil, fmt.Sprintf("Invalid request method: %s", r.Method), 401}
	}
	resURL, err := createShortenedURL(r)
	if err != nil {
		// TODO handle this case better by distinguishing between
		// bad requests and e.g. datastore errors
		return &appError{err, err.Error(), 400}
	}

	absResURL := fmt.Sprintf("http://%s/%s", r.Host, resURL)
	resp := &AddSuccessResponse{true, absResURL}
	respJSON, _ := json.Marshal(resp)
	w.Write(respJSON)
	return nil
}

func handleResolve(w http.ResponseWriter, r *http.Request, apiKey APIKey) *appError {
	if r.Method != "GET" {
		return &appError{nil, fmt.Sprintf("Invalid request method: %s", r.Method), 401}
	}

	reqPath := r.FormValue("path")
	if reqPath == "" {
		return &appError{nil, "The `path` parameter is required. ", 401}
	}
	c := appengine.NewContext(r)
	linkResults, err := getMatchingLink(reqPath, c)

	var resp *ResolveResponse

	if err != nil || len(linkResults) != 1 {
		resp = &ResolveResponse{false, Link{}}
	} else {
		resp = &ResolveResponse{true, linkResults[0]}
	}
	respJSON, _ := json.Marshal(resp)
	w.Write(respJSON)
	return nil
}

func APIHandler(w http.ResponseWriter, r *http.Request) *appError {
	apiKey := r.FormValue("apiKey")
	if apiKey == "" {
		return &appError{nil, "Invalid API Key", 401}
	}

	c := appengine.NewContext(r)
	results := make([]APIKey, 0, 1)
	_, err := datastore.NewQuery("APIKey").Filter("APIKey =", apiKey).GetAll(c, &results)
	if err != nil {
		return &appError{err, "Error validating API key", 500}
	} else if len(results) == 0 {
		return &appError{nil, "Invalid API key.", 401}
	}

	apiKeyStruct := results[0]
	handler, ok := apiRoutes[r.URL.Path]
	if !ok {
		return &appError{nil, fmt.Sprintf("No API handler for %s", r.URL.Path), 404}
	}

	return handler(w, r, apiKeyStruct)
}
