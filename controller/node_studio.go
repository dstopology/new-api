package controller

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const nodeStudioHandoffTTLSeconds int64 = 120

var nodeStudioHandoffTemplate = template.Must(template.New("node-studio-handoff").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="referrer" content="no-referrer">
  <title>Node 工作室</title>
  <style>
    body{margin:0;min-height:100vh;display:grid;place-items:center;background:#fff;color:#171717;font:14px system-ui,sans-serif}
    main{display:flex;flex-direction:column;align-items:center;gap:12px;padding:24px;text-align:center}
    button{border:1px solid #d4d4d4;border-radius:6px;background:#171717;color:#fff;padding:9px 14px;font:inherit;cursor:pointer}
  </style>
</head>
<body>
  <main>
    <p>正在进入 Node 工作室...</p>
    <form id="node-studio-handoff" method="post" action="{{.Action}}">
      <input type="hidden" name="payload" value="{{.Payload}}">
      <button type="submit">继续</button>
    </form>
  </main>
  <script>document.getElementById('node-studio-handoff').submit()</script>
</body>
</html>`))

type nodeStudioHandoffView struct {
	Action  string
	Payload string
}

func nodeStudioAPIBaseURL(c *gin.Context) string {
	if configured := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/"); configured != "" {
		return configured
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + c.Request.Host
}

func NodeStudioHandoff(c *gin.Context) {
	settings := system_setting.GetNodeStudioSettings()
	if !settings.Enabled {
		c.Status(http.StatusNotFound)
		return
	}
	if strings.TrimSpace(settings.Secret) == "" || system_setting.ValidateNodeStudioURL(settings.URL) != nil {
		c.String(http.StatusServiceUnavailable, "Node 工作室尚未配置完成")
		return
	}

	userID := c.GetInt("id")
	if userID == 0 {
		c.Redirect(http.StatusFound, "/sign-in")
		return
	}
	user, err := model.GetUserById(userID, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		c.String(http.StatusForbidden, "用户不可用")
		return
	}

	accessToken, err := user.EnsureAccessToken()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	now := common.GetTimestamp()
	payload := &dto.NodeStudioHandoffPayload{
		Version:    service.NodeStudioHandoffVersion,
		IssuedAt:   now,
		ExpiresAt:  now + nodeStudioHandoffTTLSeconds,
		APIBaseURL: nodeStudioAPIBaseURL(c),
		User: dto.NodeStudioHandoffUser{
			ID:          user.Id,
			Username:    user.Username,
			AccessToken: accessToken,
		},
	}
	encrypted, err := service.EncryptNodeStudioHandoff(payload, settings.Secret)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var page bytes.Buffer
	if err = nodeStudioHandoffTemplate.Execute(&page, nodeStudioHandoffView{
		Action:  strings.TrimSpace(settings.URL),
		Payload: encrypted,
	}); err != nil {
		common.ApiError(c, err)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/html; charset=utf-8", page.Bytes())
}
