package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func TestHasValidRegistrationCredentials(t *testing.T) {
	testCases := []struct {
		name     string
		username string
		password string
		want     bool
	}{
		{name: "letters digits and allowed punctuation", username: "Alice1@example.com", password: "Secure9@Pass.", want: true},
		{name: "single letter username", username: "A", password: "abcdefgh", want: true},
		{name: "empty username", username: "", password: "abcdefgh", want: false},
		{name: "username digit", username: "alice1", password: "abcdefgh", want: true},
		{name: "username underscore", username: "alice_name", password: "abcdefgh", want: false},
		{name: "username whitespace", username: "alice name", password: "abcdefgh", want: false},
		{name: "username unicode", username: "alice\u7528\u6237", password: "abcdefgh", want: false},
		{name: "username too long", username: strings.Repeat("a", registrationUsernameMaxBytes+1), password: "abcdefgh", want: false},
		{name: "password too short", username: "alice", password: "abcdefg", want: false},
		{name: "password too long", username: "alice", password: strings.Repeat("a", registrationPasswordMaxBytes+1), want: false},
		{name: "password digit", username: "alice", password: "abcdefg1", want: true},
		{name: "password symbol outside whitelist", username: "alice", password: "abcdefg!", want: false},
		{name: "password whitespace", username: "alice", password: "abcd efgh", want: false},
		{name: "password unicode", username: "alice", password: "abcdefg\u5bc6", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			user := model.User{Username: testCase.username, Password: testCase.password}
			if got := hasValidRegistrationCredentials(&user); got != testCase.want {
				t.Fatalf("hasValidRegistrationCredentials() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestRegisterRejectsInvalidCredentialsBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldRegisterEnabled := common.RegisterEnabled
	oldPasswordRegisterEnabled := common.PasswordRegisterEnabled
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	t.Cleanup(func() {
		common.RegisterEnabled = oldRegisterEnabled
		common.PasswordRegisterEnabled = oldPasswordRegisterEnabled
	})

	testCases := []struct {
		name string
		body string
	}{
		{name: "empty username", body: `{"username":"","password":"abcdefgh"}`},
		{name: "username underscore", body: `{"username":"alice_name","password":"abcdefgh"}`},
		{name: "password symbol", body: `{"username":"alice","password":"abcdefg!"}`},
		{name: "oversized request", body: `{"username":"` + strings.Repeat("a", int(maxRegistrationBodyBytes)) + `","password":"abcdefgh"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(testCase.body))

			Register(context)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			if !strings.Contains(recorder.Body.String(), `"success":false`) {
				t.Fatalf("response = %s, want unsuccessful response", recorder.Body.String())
			}
		})
	}
}
