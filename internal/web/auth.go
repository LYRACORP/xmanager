package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const sessionCookieName = "xm_session"

type ctxKey int

const ctxSessionKey ctxKey = 0

type session struct {
	UserID   uint
	Username string
	Role     string
	created  time.Time
}

type sessionStore struct {
	mu   sync.RWMutex
	data map[string]*session
}

func newSessionStore() *sessionStore {
	s := &sessionStore{data: make(map[string]*session)}
	go s.gcLoop()
	return s
}

func (s *sessionStore) create(userID uint, username, role string) string {
	token := randomToken()
	s.mu.Lock()
	s.data[token] = &session{UserID: userID, Username: username, Role: role, created: time.Now()}
	s.mu.Unlock()
	return token
}

func (s *sessionStore) get(token string) (*session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.data[token]
	return sess, ok
}

func (s *sessionStore) delete(token string) {
	s.mu.Lock()
	delete(s.data, token)
	s.mu.Unlock()
}

func (s *sessionStore) gcLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		s.mu.Lock()
		for k, v := range s.data {
			if time.Since(v.created) > 24*time.Hour {
				delete(s.data, k)
			}
		}
		s.mu.Unlock()
	}
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func withSession(ctx context.Context, s *session) context.Context {
	return context.WithValue(ctx, ctxSessionKey, s)
}

func sessionFromCtx(ctx context.Context) *session {
	s, _ := ctx.Value(ctxSessionKey).(*session)
	return s
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func (h *handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		sess, ok := h.sess.get(c.Value)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(withSession(r.Context(), sess)))
	}
}
