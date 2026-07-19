package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestTurnstileCheckAndConsumeUsesVerifiedSessionOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldTurnstileCheckEnabled := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() {
		common.TurnstileCheckEnabled = oldTurnstileCheckEnabled
	})

	router := gin.New()
	store := cookie.NewStore([]byte("turnstile-test-secret"))
	router.Use(sessions.Sessions("turnstile-test", store))
	router.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("turnstile", true)
		if err := session.Save(); err != nil {
			t.Fatalf("seed session: %v", err)
		}
		c.Status(http.StatusNoContent)
	})

	handlerCalls := 0
	router.POST("/register", TurnstileCheckAndConsume(), func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusNoContent)
	})

	seedRecorder := httptest.NewRecorder()
	seedRequest := httptest.NewRequest(http.MethodGet, "/seed", nil)
	router.ServeHTTP(seedRecorder, seedRequest)
	seedCookies := seedRecorder.Result().Cookies()
	if len(seedCookies) == 0 {
		t.Fatal("seed response did not set a session cookie")
	}

	firstRecorder := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/register", nil)
	firstRequest.AddCookie(seedCookies[0])
	router.ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusNoContent {
		t.Fatalf("first registration status = %d, want %d", firstRecorder.Code, http.StatusNoContent)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls after first registration = %d, want 1", handlerCalls)
	}

	consumedCookies := firstRecorder.Result().Cookies()
	if len(consumedCookies) == 0 {
		t.Fatal("first registration response did not update the session cookie")
	}
	secondRecorder := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/register", nil)
	secondRequest.AddCookie(consumedCookies[0])
	router.ServeHTTP(secondRecorder, secondRequest)

	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second registration status = %d, want %d", secondRecorder.Code, http.StatusOK)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls after second registration = %d, want 1", handlerCalls)
	}
}
