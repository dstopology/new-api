package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type ExternalSkill struct {
	Id          int    `json:"id"`
	Name        string `json:"name" gorm:"size:128;not null;uniqueIndex"`
	Description string `json:"description" gorm:"type:text"`
	Content     string `json:"content" gorm:"type:text;not null"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

type ExternalSkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ExternalSkillDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

func (skill *ExternalSkill) Insert() error {
	now := common.GetTimestamp()
	skill.CreatedTime = now
	skill.UpdatedTime = now
	return DB.Create(skill).Error
}

func (skill *ExternalSkill) Update() error {
	skill.UpdatedTime = common.GetTimestamp()
	return DB.Model(&ExternalSkill{}).
		Where("id = ?", skill.Id).
		Updates(map[string]any{
			"name":         skill.Name,
			"description":  skill.Description,
			"content":      skill.Content,
			"updated_time": skill.UpdatedTime,
		}).Error
}

func GetExternalSkillByID(id int) (*ExternalSkill, error) {
	var skill ExternalSkill
	err := DB.First(&skill, id).Error
	return &skill, err
}

func GetExternalSkillByName(name string) (*ExternalSkill, error) {
	var skill ExternalSkill
	err := DB.Where("name = ?", name).First(&skill).Error
	return &skill, err
}

func ListExternalSkillSummaries() ([]ExternalSkillSummary, error) {
	var skills []ExternalSkillSummary
	err := DB.Model(&ExternalSkill{}).
		Select("name", "description").
		Order("name ASC").
		Find(&skills).Error
	return skills, err
}

func SearchExternalSkills(keyword string, offset, limit int) ([]ExternalSkill, int64, error) {
	db := DB.Model(&ExternalSkill{})
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("name LIKE ? OR description LIKE ?", like, like)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var skills []ExternalSkill
	err := db.Order("id DESC").Offset(offset).Limit(limit).Find(&skills).Error
	return skills, total, err
}

func DeleteExternalSkill(id int) error {
	return DB.Delete(&ExternalSkill{}, id).Error
}
