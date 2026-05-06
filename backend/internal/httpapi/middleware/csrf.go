package middleware

import "net/http"

// CSRF enforces the SameSite=Lax + custom-header pattern the PLAN calls out:
// every mutating verb (POST/PUT/PATCH/DELETE) must carry X-Requested-With,
// which a cross-site form submission can't forge without CORS preflight.
//
// Reads (GET/HEAD/OPTIONS) are exempt — they don't change state, and forcing
// the header on every fetch breaks browsers loading static assets.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-Requested-With") == "" {
			writeJSONError(w, http.StatusForbidden, "missing X-Requested-With")
			return
		}
		next.ServeHTTP(w, r)
	})
}
