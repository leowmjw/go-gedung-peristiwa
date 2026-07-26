package demo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	demopkg "github.com/leow/go-gedung-peristiwa/internal/demo"
)

type sessionContextKey struct{}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := ""
		if c, err := r.Cookie(demopkg.SessionCookieName); err == nil && c.Value != "" {
			sid = c.Value
		}
		if sid == "" {
			var err error
			sid, err = newSessionID()
			if err != nil {
				http.Error(w, "session init failed", http.StatusInternalServerError)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     demopkg.SessionCookieName,
				Value:    sid,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}
		s.sessions.Touch(sid)
		ctx := context.WithValue(r.Context(), sessionContextKey{}, sid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sessionID(r *http.Request) string {
	sid, _ := r.Context().Value(sessionContextKey{}).(string)
	return sid
}
