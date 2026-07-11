package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type externalSkillInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

func normalizeExternalSkillInput(input *externalSkillInput) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
}

func validateExternalSkillInput(input externalSkillInput) string {
	if input.Name == "" {
		return "skill name is required"
	}
	if len(input.Name) > 128 {
		return "skill name must not exceed 128 characters"
	}
	if strings.TrimSpace(input.Content) == "" {
		return "skill content is required"
	}
	return ""
}

func ListExternalSkills(c *gin.Context) {
	skills, err := model.ListExternalSkillSummaries()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, skills)
}

func GetExternalSkill(c *gin.Context) {
	skill, err := model.GetExternalSkillByName(c.Param("name"))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "external skill not found"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, model.ExternalSkillDetail{
		Name:        skill.Name,
		Description: skill.Description,
		Content:     skill.Content,
	})
}

func GetExternalSkillContent(c *gin.Context) {
	skill, err := model.GetExternalSkillByName(c.Param("name"))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.String(http.StatusNotFound, "external skill not found")
			return
		}
		common.ApiError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(skill.Content))
}

func AdminListExternalSkills(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	skills, total, err := model.SearchExternalSkills(c.Query("keyword"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(skills)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetExternalSkill(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	skill, err := model.GetExternalSkillByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, skill)
}

func AdminCreateExternalSkill(c *gin.Context) {
	var input externalSkillInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	normalizeExternalSkillInput(&input)
	if message := validateExternalSkillInput(input); message != "" {
		common.ApiErrorMsg(c, message)
		return
	}

	skill := model.ExternalSkill{
		Name:        input.Name,
		Description: input.Description,
		Content:     input.Content,
	}
	if err := skill.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &skill)
}

func AdminUpdateExternalSkill(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var input externalSkillInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	normalizeExternalSkillInput(&input)
	if message := validateExternalSkillInput(input); message != "" {
		common.ApiErrorMsg(c, message)
		return
	}

	skill := model.ExternalSkill{
		Id:          id,
		Name:        input.Name,
		Description: input.Description,
		Content:     input.Content,
	}
	if err := skill.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	updated, err := model.GetExternalSkillByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, updated)
}

func AdminDeleteExternalSkill(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteExternalSkill(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
