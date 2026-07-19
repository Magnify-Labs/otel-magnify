package api

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

type databaseReadinessChecker interface {
	PingContext(context.Context) error
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	//nolint:errcheck,gosec // status already committed; a Write failure here only signals a closed client connection
	w.Write([]byte("ok"))
}

func readinessHandler(db ext.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isNilDatabaseStore(db) {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}

		checker, ok := db.(databaseReadinessChecker)
		if !ok {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := checker.PingContext(ctx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		//nolint:errcheck,gosec // status already committed; a Write failure here only signals a closed client connection
		w.Write([]byte("ready"))
	}
}

func isNilDatabaseStore(db ext.Store) bool {
	if db == nil {
		return true
	}

	value := reflect.ValueOf(db)
	//nolint:exhaustive // Only kinds accepted by Value.IsNil are relevant here.
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
